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
	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
	"github.com/mgoltzsche/k8storagex/internal/utils"
	corev1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	provisioner               = "provisioner"
	annPVCName                = "k8storagex.mgoltzsche.github.com/pvc-name"
	annPVCNamespace           = "k8storagex.mgoltzsche.github.com/pvc-namespace"
	annStorageProvisioner     = "volume.beta.kubernetes.io/storage-provisioner"
	annStorageProvisionerSpec = "k8storagex.mgoltzsche.github.com/provisioner-spec"
	annProvisionedBy          = "pv.kubernetes.io/provisioned-by"
	annSelectedNode           = "volume.kubernetes.io/selected-node"
	finalizer                 = "k8storagex.mgoltzsche.github.com/finalizer"
	finalizerPVProtection     = "kubernetes.io/pv-protection"
	KeyNode                   = "kubernetes.io/hostname"
	finalizerPVCProtection    = "kubernetes.io/pvc-protection"
)

// PersistentVolumeClaimReconciler reconciles a Cache object
type PersistentVolumeClaimReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	ManagerNamespace string
	Provisioners     Provisioners
	jobReconciler    *utils.JobReconciler
	recorder         record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *PersistentVolumeClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("PersistentVolumeClaim")
	r.jobReconciler = &utils.JobReconciler{
		Client:                   r.Client,
		Recorder:                 r.recorder,
		AnnotationOwnerName:      annPVCName,
		AnnotationOwnerNamespace: annPVCNamespace,
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.PersistentVolumeClaim{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, utils.PodToRequestMapper(r.ManagerNamespace, annPVCName, annPVCNamespace)).
		Complete(r)
}

// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;create;update;patch;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;create;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes/finalizers,verbs=update
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;create;delete;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *PersistentVolumeClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("persistentvolumeclaim", req.NamespacedName.String())

	// Get PVC
	claim := &corev1.PersistentVolumeClaim{}
	err := r.Client.Get(ctx, req.NamespacedName, claim)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if claim.DeletionTimestamp != nil {
		if utils.HasString(claim.Finalizers, finalizerPVCProtection) {
			if claim.Annotations[storageapi.AnnotationPersistentVolumeClaimNoProtection] == storageapi.Enabled {
				// Remove pvc-protection finalizers
				utils.RemoveString(&claim.Finalizers, finalizerPVCProtection)
				return ctrl.Result{}, r.Client.Status().Update(ctx, claim)
			}
			return ctrl.Result{}, nil
		}
		// Clean up pending/failed provisioner Pods and their (partially created) volumes
		if utils.HasString(claim.Finalizers, finalizer) {
			if len(claim.Finalizers) != 1 {
				// Don't trigger deprovisioner as long as PersistentVolumeClaim is used by a Pod
				log.V(1).Info("Skipping PersistentVolumeClaim deletion since external finalizers are still present")
				return ctrl.Result{}, nil
			}
			podName := utils.ResourceName(pvNameForPVC(claim), provisioner)
			done, err := r.deletePod(ctx, podName, log)
			if err != nil || !done {
				return ctrl.Result{}, err
			}
			done, err = r.deprovision(ctx, claim, log)
			if err != nil || !done {
				return ctrl.Result{}, err
			}
			utils.RemoveString(&claim.Finalizers, finalizer)
			return ctrl.Result{}, r.Client.Status().Update(ctx, claim)
		}
		return ctrl.Result{}, nil
	}

	// Get provisioner
	provisionerSpec := resolveProvisioner(claim, r.Provisioners)
	if provisionerSpec == nil {
		return ctrl.Result{}, nil
	}

	log = log.WithValues("provisioner", provisionerSpec.GetProvisionerName())
	log.V(1).Info("Reconciling PersistentVolumeClaim")

	// Ignore PVC when provisioner or StorageClass does not match
	should, err := r.shouldProvision(ctx, claim, provisionerSpec, log)
	if err != nil || !should {
		return ctrl.Result{}, err
	}

	// Add finalizer in order to clean up pending/failed Pods in case the PVC is deleted before the provisioning succeeded
	if utils.AddString(&claim.Finalizers, finalizer) {
		log.V(1).Info("Adding finalizer to PersistentVolumeClaim")
		return ctrl.Result{}, r.Client.Status().Update(ctx, claim)
	}

	// Run provisioner Pod and create PersistentVolume
	done, err := r.provision(ctx, claim, provisionerSpec, log)
	if err != nil || !done {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func resolveProvisioner(o metav1.Object, provisioners Provisioners) *storageapi.StorageProvisioner {
	a := o.GetAnnotations()
	if a == nil {
		return nil
	}
	return provisioners.Get(a[annStorageProvisioner])
}

func (r *PersistentVolumeClaimReconciler) deprovision(ctx context.Context, claim *corev1.PersistentVolumeClaim, log logr.Logger) (done bool, err error) {
	pvName := types.NamespacedName{Name: pvNameForPVC(claim)}
	pv := &corev1.PersistentVolume{}
	err = r.Client.Get(ctx, pvName, pv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// TODO: run volume deprovisioner pod to clean up what the interrupted provisioner may have left behind
			return true, nil // PersistentVolume doesn't exist anymore
		}
		return false, err
	}
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
		log = log.WithValues("persistentvolume", pv.Name)
		log.V(1).Info(fmt.Sprintf("Skipping PersistentVolume deletion since reclaim policy is %s", pv.Spec.PersistentVolumeReclaimPolicy))
		return true, nil
	}
	log.V(1).Info("Deleting PersistentVolume")
	err = r.Client.Delete(ctx, pv)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	return true, nil // marked PersistentVolume as deleted, not waiting for it to be actually deleted
}

func (r *PersistentVolumeClaimReconciler) provision(ctx context.Context, claim *corev1.PersistentVolumeClaim, provisionerSpec *storageapi.StorageProvisioner, log logr.Logger) (done bool, err error) {
	pv := &corev1.PersistentVolume{}
	pvName := types.NamespacedName{Name: pvNameForPVC(claim)}
	err = r.Client.Get(ctx, pvName, pv)
	pvExists := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err // unexpected error occured
	}

	env, errAnn := utils.AnnotationsToEnv(claim, provisionerSpec.Spec.Env)
	if errAnn != nil {
		r.recorder.Eventf(claim, corev1.EventTypeWarning, "AnnotationMissing", err.Error())
	}

	nodeName := claim.Annotations[annSelectedNode]
	log = log.WithValues("persistentvolume", pvName.Name)
	if nodeName == "" {
		log.Info("PersistentVolumeClaim is not assigned to a node")
	} else {
		log = log.WithValues("node", nodeName)
	}

	podName := types.NamespacedName{
		Name:      utils.ResourceName(pvNameForPVC(claim), provisioner),
		Namespace: r.ManagerNamespace,
	}
	done, err = r.jobReconciler.ReconcileJob(utils.JobRequest{
		Context:   ctx,
		Name:      provisioner,
		PodName:   podName,
		Owner:     claim,
		ShouldRun: !pvExists && nodeName != "" && errAnn == nil,
		Create: func() (*corev1.Pod, error) {
			nodePath, err := utils.StorageRootPathForNode(nodeName, provisionerSpec.Spec.Nodes)
			if err != nil {
				return nil, fmt.Errorf("invalid storageprovisioner: %w", err)
			}
			err = utils.SubstituteProvisionerPlaceholders(provisionerSpec, utils.ProvisionerParams{
				NodeName:              nodeName,
				NodePath:              nodePath,
				PersistentVolumeName:  pvName.Name,
				PersistentVolumeClaim: types.NamespacedName{Name: claim.Name, Namespace: claim.Namespace},
			})
			if err != nil {
				return nil, err
			}
			pod, err := utils.NewProvisionerPod(utils.PodSource{
				ContainerName: provisioner,
				PodName: types.NamespacedName{
					Name:      utils.ResourceName(pvNameForPVC(claim), provisioner),
					Namespace: r.ManagerNamespace,
				},
				SubstitutedProvisioner: provisionerSpec,
				Container:              &provisionerSpec.Spec.Containers.Provisioner,
				Env:                    env,
			})
			if err != nil {
				return nil, err
			}
			utils.CopyAnnotations(claim, pod, provisionerSpec.Spec.Env)
			pod.Annotations[annStorageProvisionerSpec], err = utils.StorageProvisionerToJSON(provisionerSpec)

			r.recorder.Event(claim, corev1.EventTypeNormal, "Provisioning", "Provisioning PersistentVolume")

			return pod, nil
		},
		OnCompleted: func(pod *corev1.Pod) (done bool, err error) {
			// Create PersistentVolume after Pod succeeded and before Pod gets deleted
			provisionerJSON := pod.Annotations[annStorageProvisionerSpec]
			provisionerSpec, err = utils.StorageProvisionerFromJSON(provisionerJSON)
			if err != nil {
				return true, fmt.Errorf("invalid/missing provisioner pod annotation %s: %w", annStorageProvisionerSpec, err)
			}
			pv = r.persistentVolumeForClaim(claim, provisionerSpec, provisionerJSON)
			utils.CopyAnnotations(pod, pv, provisionerSpec.Spec.Env)
			if hostPath := pv.Spec.HostPath; hostPath != nil {
				log = log.WithValues("path", hostPath.Path)
			}
			err = r.Client.Create(ctx, pv)
			if err != nil {
				return false, err
			}
			log.Info("Successfully provisioned PersistentVolume")
			msg := fmt.Sprintf("Provisioned PersistentVolume %s", pvName.Name)
			r.recorder.Eventf(claim, corev1.EventTypeNormal, "Provisioned", msg)
			return true, nil
		},
		Log: log,
	})
	if done && err != nil {
		log.Error(err, "Failed to provision PersistentVolume")
		msg := fmt.Sprintf("Failed to provision PersistentVolume %s: %v", pvName.Name, err)
		r.recorder.Eventf(claim, corev1.EventTypeWarning, "ProvisionerFailed", msg)
	}
	return done, err
}

func (r *PersistentVolumeClaimReconciler) persistentVolumeForClaim(claim *corev1.PersistentVolumeClaim, provisioner *storageapi.StorageProvisioner, provisionerJSON string) *corev1.PersistentVolume {
	pv := &corev1.PersistentVolume{Spec: provisioner.Spec.PersistentVolumeTemplate}
	pv.Name = pvNameForPVC(claim)
	pv.Annotations = map[string]string{
		annStorageProvisioner:     provisioner.Spec.Name,
		annStorageProvisionerSpec: provisionerJSON,
		annProvisionedBy:          "k8storagex",
	}
	pv.Finalizers = []string{finalizer}
	if pv.Spec.Capacity == nil {
		pv.Spec.Capacity = claim.Spec.Resources.Requests
	}
	if claim.Spec.StorageClassName != nil {
		pv.Spec.StorageClassName = *claim.Spec.StorageClassName
	}
	pv.Spec.ClaimRef = &corev1.ObjectReference{
		APIVersion:      "v1",
		Kind:            "PersistentVolumeClaim",
		Name:            claim.Name,
		Namespace:       claim.Namespace,
		UID:             claim.UID,
		ResourceVersion: claim.ResourceVersion,
	}
	return pv
}

func pvNameForPVC(claim *corev1.PersistentVolumeClaim) string {
	return fmt.Sprintf("pvc-%s", string(claim.UID))
}

func (r *PersistentVolumeClaimReconciler) deletePod(ctx context.Context, name string, log logr.Logger) (done bool, err error) {
	podName := types.NamespacedName{Name: name, Namespace: r.ManagerNamespace}
	return utils.DeletePod(ctx, r.Client, podName, log)
}

// shouldProvision returns whether a claim should have a volume provisioned for
// it, i.e. whether a Provision is "desired"
func (r *PersistentVolumeClaimReconciler) shouldProvision(ctx context.Context, claim *corev1.PersistentVolumeClaim, provisioner *storageapi.StorageProvisioner, log logr.Logger) (bool, error) {
	if claim.Spec.VolumeName != "" || claim.Spec.StorageClassName == nil {
		return false, nil // already provisioned or missing storageclass
	}

	// Get StorageClass
	claimClass := persistentVolumeClaimClass(claim)
	class, err := r.getStorageClass(ctx, claimClass)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Error(err, "PersistentVolumeClaim spec references non-existing StorageClass")
			r.recorder.Eventf(claim, corev1.EventTypeWarning, "StorageClassNotFound", err.Error())
			return false, nil
		}
		return false, err
	}

	// Check volumeMode
	pvMode := *provisioner.Spec.PersistentVolumeTemplate.VolumeMode
	if claim.Spec.VolumeMode == nil || pvMode != *claim.Spec.VolumeMode {
		mode := "nil"
		if claim.Spec.VolumeMode != nil {
			mode = fmt.Sprintf("%q", *claim.Spec.VolumeMode)
		}
		r.recorder.Eventf(claim, corev1.EventTypeWarning, "InvalidVolumeMode", "Invalid volume mode %s, expected %s", mode, pvMode)
		return false, nil
	}

	// Check volumeBindingMode and wait for first consumer eventually
	if class.VolumeBindingMode != nil && *class.VolumeBindingMode == storage.VolumeBindingWaitForFirstConsumer {
		// When claim is in delay binding mode, annSelectedNode is
		// required to provision volume.
		// Though PV controller set annStorageProvisioner only when
		// annSelectedNode is set, but provisioner may remove
		// annSelectedNode to notify scheduler to reschedule again.
		if selectedNode, ok := claim.Annotations[annSelectedNode]; ok && selectedNode != "" {
			return true, nil // Claim has node assigned
		}
		log.Info("Waiting for first consumer to bind to a node")
		return false, nil
	}
	return true, nil // no delay binding, this controller has to select a node
}

func (r *PersistentVolumeClaimReconciler) getStorageClass(ctx context.Context, name string) (*storage.StorageClass, error) {
	sc := &storage.StorageClass{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name}, sc)
	return sc, err
}

func persistentVolumeClaimClass(claim *corev1.PersistentVolumeClaim) string {
	// Use beta annotation first
	if class, found := claim.Annotations[corev1.BetaStorageClassAnnotation]; found {
		return class
	}
	if claim.Spec.StorageClassName != nil {
		return *claim.Spec.StorageClassName
	}
	return ""
}
