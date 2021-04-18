/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/mgoltzsche/cache-provisioner/internal/utils"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	deprovisioner             = "deprovisioner"
	annPVName                 = "cache-provisioner.mgoltzsche.github.com/pv-name"
	annPVDisableDeprovisioner = "cache-provisioner.mgoltzsche.github.com/pv-deprovisioner-disabled"
)

// PersistentVolumeReconciler reconciles a Cache object
type PersistentVolumeReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	ManagerNamespace string
	Provisioners     Provisioners
	jobReconciler    *utils.JobReconciler
	recorder         record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *PersistentVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("PersistentVolume")
	r.jobReconciler = &utils.JobReconciler{
		Client:              r.Client,
		Recorder:            r.recorder,
		AnnotationOwnerName: annPVName,
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.PersistentVolume{}, builder.WithPredicates(predicate.And(
			hasDeletionTimestamp(),
			hasSupportedProvisionerOrFinalizer(r.Provisioners),
		))).
		Watches(&source.Kind{Type: &corev1.Pod{}}, utils.PodToRequestMapper(r.ManagerNamespace, annPVName, "")).
		//Watches(&source.Kind{Type: &corev1.PersistentVolumeClaim{}}, pvcToRequestMapper()).
		Complete(r)
}

// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;update;delete;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumes/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=persistentvolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;create;delete;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *PersistentVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("persistentvolume", req.NamespacedName.Name)

	log.V(1).Info("Reconciling PersistentVolume")

	// Get PV
	pv := &corev1.PersistentVolume{}
	err := r.Client.Get(ctx, req.NamespacedName, pv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if pv.DeletionTimestamp != nil {
		// Free host path storage on PersistentVolume deletion
		if !utils.HasString(pv.Finalizers, finalizer) {
			return ctrl.Result{}, nil
		}
		// Check if PersistentVolume is not bound to a PersistentVolumeClaim
		should, err := r.canDeprovision(ctx, pv, log)
		if err != nil || !should {
			return ctrl.Result{}, err
		}

		// Run deprovisioner Pod
		done, err := r.deprovisionVolume(ctx, pv, log)
		if err != nil || !done {
			return ctrl.Result{}, err
		}

		// Delete PersistentVolume
		utils.RemoveString(&pv.Finalizers, finalizer)
		err = r.Client.Status().Update(ctx, pv)
		if err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Deleted PersistentVolume")
		r.recorder.Eventf(pv, corev1.EventTypeNormal, "Deprovisioned", "Deprovisioned PersistentVolume")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PersistentVolumeReconciler) canDeprovision(ctx context.Context, pv *corev1.PersistentVolume, log logr.Logger) (done bool, err error) {
	for i := 0; i < 2; i++ {
		if pv.Status.Phase == corev1.VolumeBound {
			return false, nil // Volume is bound, don't deprovision
		}
		claimRef := pv.Spec.ClaimRef
		if claimRef == nil {
			return true, nil // no claim referenced, can be deprovisioned
		}
		// Make sure PVC doesn't exit anymore
		pvcName := types.NamespacedName{Name: claimRef.Name, Namespace: claimRef.Namespace}
		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Client.Get(ctx, pvcName, pvc)
		if err == nil || !apierrors.IsNotFound(err) {
			if err == nil {
				log.V(1).Info("Waiting for PersistentVolumeClaim to be deleted before deprovisioning PersistentVolume", "persistentvolumeclaim", pvcName.String())
			}
			break
		}

		// Verify that the PersistentVolume is managed by a known StorageProvisioner
		if resolveProvisioner(pv, r.Provisioners) == nil {
			return false, nil // not managed by known StorageProvisioner
		}

		// Remove claimRef
		pv.Spec.ClaimRef = nil
		err = r.Client.Update(ctx, pv)
		if err != nil {
			if apierrors.IsConflict(err) {
				// Try to resolve expected conflict error here once to avoid polluting the logs
				pvName := types.NamespacedName{Name: pv.Name, Namespace: pv.Namespace}
				e := r.Client.Get(ctx, pvName, pv)
				if e != nil {
					err = e
					break
				}
				continue
			}
			break
		}
		log.V(1).Info("Removed claimRef from PersistentVolume")
		return false, nil // claimRef removed, let next reconciliation run step out earlier and continue deprovisioning
	}
	return false, errors.Wrap(err, "remove claimRef from persistentvolume")
}

func (r *PersistentVolumeReconciler) deprovisionVolume(ctx context.Context, pv *corev1.PersistentVolume, log logr.Logger) (done bool, err error) {
	log = log.WithValues("persistentvolume", pv.Name)

	provisionerJSON := pv.Annotations[annStorageProvisionerSpec]
	if provisionerJSON == "" {
		err := fmt.Errorf("missing annotation %s", annStorageProvisionerSpec)
		log.Error(err, "Cannot derive deprovisioner for PersistentVolume")
		r.recorder.Eventf(pv, corev1.EventTypeWarning, "DeprovisionerSpecAnnotationMissing", err.Error())
		return true, nil
	}
	provisioner, err := utils.StorageProvisionerFromJSON(provisionerJSON)
	if err != nil {
		err = errors.Wrapf(err, "invalid deprovisioner annotation %s on PersistentVolume", annStorageProvisionerSpec)
		log.Error(err, "Cannot derive deprovisioner from PersistentVolume")
		r.recorder.Eventf(pv, corev1.EventTypeWarning, "DeprovisionerSpecAnnotationInvalid", err.Error())
		return false, nil
	}

	log = log.WithValues("provisioner", provisioner.GetProvisionerName())

	podName := types.NamespacedName{
		Name:      utils.ResourceName(pv.Name, deprovisioner),
		Namespace: r.ManagerNamespace,
	}
	done, err = r.jobReconciler.ReconcileJob(utils.JobRequest{
		Context:   ctx,
		Name:      deprovisioner,
		PodName:   podName,
		Owner:     pv,
		ShouldRun: pv.Annotations != nil && pv.Annotations[annPVDisableDeprovisioner] != "true",
		Create: func() (*corev1.Pod, error) {
			env, err := utils.AnnotationsToEnv(pv, provisioner.Spec.Env)
			if err != nil {
				return nil, errors.Wrap(err, "persistentvolume does not specify annotation")
			}

			pod, err := utils.NewProvisionerPod(utils.PodSource{
				ContainerName:          deprovisioner,
				PodName:                podName,
				SubstitutedProvisioner: provisioner,
				Container:              &provisioner.Spec.Containers.Deprovisioner,
				Env:                    env,
			})
			if err != nil {
				return nil, err
			}

			return pod, nil
		},
		OnCompleted: func(_ *corev1.Pod) (done bool, err error) {
			pv.Annotations[annPVDisableDeprovisioner] = "true"
			err = r.Client.Update(ctx, pv)
			if err != nil {
				return false, err
			}
			return true, nil
		},
		Log: log,
	})
	if done && err != nil {
		log = log.WithValues("pod", podName.String())
		log.Error(err, "Failed to deprovision PersistentVolume")
		msg := fmt.Sprintf("Failed to deprovision PersistentVolume: %v", err)
		r.recorder.Eventf(pv, corev1.EventTypeWarning, "DeprovisionerFailed", msg)
	}
	return done, err
}

func hostPathFromPV(pv *corev1.PersistentVolume) string {
	hostPath := pv.Spec.HostPath
	if hostPath == nil {
		return ""
	}
	return hostPath.Path
}

func nodeNameFromPV(pv *corev1.PersistentVolume) (string, error) {
	nodeAffinity := pv.Spec.NodeAffinity
	if nodeAffinity == nil {
		return "", fmt.Errorf("no NodeAffinity set")
	}
	required := nodeAffinity.Required
	if required == nil {
		return "", fmt.Errorf("no NodeAffinity.Required set")
	}

	node := ""
	for _, selectorTerm := range required.NodeSelectorTerms {
		for _, expression := range selectorTerm.MatchExpressions {
			if expression.Key == KeyNode && expression.Operator == corev1.NodeSelectorOpIn {
				if len(expression.Values) != 1 {
					return "", fmt.Errorf("multiple values for the node affinity")
				}
				node = expression.Values[0]
				break
			}
		}
		if node != "" {
			break
		}
	}
	if node == "" {
		return "", fmt.Errorf("cannot find affinited node")
	}
	return node, nil
}

func hasSupportedProvisionerOrFinalizer(provisioners Provisioners) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return resolveProvisioner(o, provisioners) != nil || utils.HasString(o.GetFinalizers(), finalizer)
	})
}

func hasDeletionTimestamp() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetDeletionTimestamp() != nil
	})
}

func pvcToRequestMapper() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		pvc, ok := o.(*corev1.PersistentVolumeClaim)
		if !ok || pvc.Spec.VolumeName == "" {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: pvc.Spec.VolumeName}}}
	})
}
