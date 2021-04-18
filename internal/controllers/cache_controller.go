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
	"time"

	"github.com/go-logr/logr"
	cacheapi "github.com/mgoltzsche/cache-provisioner/api/v1alpha1"
	"github.com/mgoltzsche/cache-provisioner/internal/utils"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	annCacheName      = "cache-provisioner.mgoltzsche.github.com/cache-name"
	annCacheNamespace = "cache-provisioner.mgoltzsche.github.com/cache-namespace"
)

// CacheReconciler reconciles a Cache object
type CacheReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	ManagerNamespace   string
	ServiceAccountName string
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cacheapi.Cache{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, utils.PodToRequestMapper(r.ManagerNamespace, annCacheName, annCacheNamespace)).
		Complete(r)
}

// TODO: implement proper Cache resource clean up and garbage collection

// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;create;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;create;watch
// +kubebuilder:rbac:groups=cache-provisioner.mgoltzsche.github.com,resources=caches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cache-provisioner.mgoltzsche.github.com,resources=caches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cache-provisioner.mgoltzsche.github.com,resources=caches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Cache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *CacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("cache", req.NamespacedName.String())

	log.Info("Reconciling Cache")

	// Get Cache
	cache := &cacheapi.Cache{}
	err := r.Client.Get(ctx, req.NamespacedName, cache)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Clean up the file systems on the nodes
	if !cache.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, cache, log)
	}

	// Add finalizer to prevent deletion before its associated resources have been cleaned up.
	if utils.AddString(&cache.Finalizers, cacheapi.CacheFinalizer) {
		return ctrl.Result{}, r.Client.Update(ctx, cache)
	}

	return ctrl.Result{}, nil
}

func (r *CacheReconciler) finalize(ctx context.Context, cache *cacheapi.Cache, log logr.Logger) error {
	err := r.freeStorage(ctx, cache, log)
	if err != nil {
		return err
	}
	// TODO: Run Pods to clean up cache resources on the nodes (untag images, eventually find and delete old containers).
	if utils.RemoveString(&cache.Finalizers, cacheapi.CacheFinalizer) {
		return r.Client.Status().Update(ctx, cache)
	}
	return nil
}

func (r *CacheReconciler) freeStorage(ctx context.Context, cache *cacheapi.Cache, log logr.Logger) error {
	podName := r.imageUntagPodName(cache)
	if cache.Status.LastReset != nil && cache.Status.LastReset.CacheGeneration == cache.Status.CacheGeneration {
		return r.deletePods(ctx, cache, podName)
	}
	podNamespacedName := types.NamespacedName{Name: podName, Namespace: r.ManagerNamespace}
	done, _, err := utils.ReconcilePod(ctx, r.Client, podNamespacedName, func() (*corev1.Pod, error) {
		// Configure Pod
		pod := &corev1.Pod{}
		pod.Annotations = map[string]string{
			annCacheName:      cache.Name,
			annCacheNamespace: cache.Namespace,
		}
		pod.Spec.ServiceAccountName = r.ServiceAccountName
		pod.Spec.RestartPolicy = corev1.RestartPolicyNever
		pod.Spec.Containers = []corev1.Container{{
			Name:  "image-untagger",
			Image: "TODO",
			Args:  []string{"untag image within registry"},
		}}

		// Lock cache
		changed := setPodsClearedCondition(cache, metav1.ConditionFalse, "PodDeletionPending", "new reset pod will be removed on success")
		if cache.Status.Phase != cacheapi.CachePhaseReject || changed {
			cache.Status.CacheGeneration++
			cache.Status.Phase = cacheapi.CachePhaseReject
			err := r.Client.Status().Update(ctx, cache)
			if err != nil {
				return nil, err
			}
			// TODO: don't return reconcile when cache doesn't exist (anymore)
			return pod, r.Client.Get(ctx, types.NamespacedName{Name: cache.Name, Namespace: cache.Namespace}, cache)
		}

		return pod, nil
	})
	if err != nil {
		changed := utils.SetCondition(&cache.Status.Conditions, metav1.Condition{
			Type:               cacheapi.ConditionStorageReset,
			Status:             metav1.ConditionFalse,
			Reason:             "PendingImageDeletion",
			Message:            err.Error(),
			ObservedGeneration: cache.Generation,
		})
		if changed {
			e := r.Client.Status().Update(ctx, cache)
			if e != nil {
				return e
			}
		}
		if done {
			return nil
		}
		return err
	}

	// On success update status, unlock cache
	utils.SetCondition(&cache.Status.Conditions, metav1.Condition{
		Type:               cacheapi.ConditionStorageReset,
		Status:             metav1.ConditionTrue,
		Reason:             "Success",
		Message:            "Cache image has been untagged",
		ObservedGeneration: cache.Generation,
	})
	cache.Status.Phase = cacheapi.CachePhaseReady
	cache.Status.LastReset = &cacheapi.ResetStatus{
		CacheGeneration: cache.Generation,
		ResetTime:       metav1.Time{Time: time.Now()},
	}
	err = r.Client.Status().Update(ctx, cache)
	if err != nil {
		return err
	}

	err = r.deletePods(ctx, cache, podName)
	if err != nil {
		return err
	}

	return nil
}

func (r *CacheReconciler) deletePods(ctx context.Context, cache *cacheapi.Cache, podNames ...string) (err error) {
	if c := utils.GetCondition(cache.Status.Conditions, cacheapi.ConditionPodsCleared); c != nil && c.Status == metav1.ConditionTrue {
		// skip Pod deletion when already happened
		return nil
	}
	for _, pod := range podNames {
		if e := r.deletePod(ctx, pod); e != nil && err == nil {
			err = e
		}
	}
	var changed bool
	if err != nil {
		changed = setPodsClearedCondition(cache, metav1.ConditionFalse, "PodDeletionFailed", err.Error())
	} else {
		changed = setPodsClearedCondition(cache, metav1.ConditionTrue, "Completed", "all pods removed")
	}
	if changed {
		if e := r.Status().Update(ctx, cache); e != nil && err == nil {
			err = e
		}
	}
	return err
}

func (r *CacheReconciler) deletePod(ctx context.Context, name string) error {
	pod := &corev1.Pod{}
	podName := types.NamespacedName{Name: name, Namespace: r.ManagerNamespace}
	err := r.Client.Get(ctx, podName, pod)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	err = r.Client.Delete(ctx, pod)
	if err == nil {
		err = errors.Errorf("pod deletion pending")
	}
	return err
}

func setPodsClearedCondition(cache *cacheapi.Cache, status metav1.ConditionStatus, reason, message string) bool {
	return utils.SetCondition(&cache.Status.Conditions, metav1.Condition{
		Type:               cacheapi.ConditionPodsCleared,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cache.Generation,
	})
}

func (r *CacheReconciler) imageUntagPodName(cache *cacheapi.Cache) string {
	// TODO: build name
	return cache.Name
}
