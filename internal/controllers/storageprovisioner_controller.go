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

	"github.com/go-logr/logr"
	storageapi "github.com/mgoltzsche/cache-provisioner/api/v1alpha1"
	"github.com/mgoltzsche/cache-provisioner/internal/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// StorageProvisionerReconciler reconciles a StorageProvisioner object
type StorageProvisionerReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	Provisioners *ProvisionerRegistry
	Namespace    string
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageProvisionerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storageapi.StorageProvisioner{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			return r.Namespace == "" || o.GetNamespace() == r.Namespace
		}))).
		Complete(r)
}

// +kubebuilder:rbac:groups=cache-provisioner.mgoltzsche.github.com,resources=storageprovisioners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cache-provisioner.mgoltzsche.github.com,resources=storageprovisioners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cache-provisioner.mgoltzsche.github.com,resources=storageprovisioners/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the StorageProvisioner object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *StorageProvisionerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("storageprovisioner", req.NamespacedName.String())

	log.Info("Reconciling StorageProvisioner")

	// Get StorageProvisioner
	config := &storageapi.StorageProvisioner{}
	err := r.Client.Get(ctx, req.NamespacedName, config)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.Provisioners.Delete(config)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Register updated configuration
	err = r.Provisioners.Put(config)
	if err != nil {
		if utils.SetCondition(&config.Status.Conditions, metav1.Condition{
			Type:               storageapi.ConditionConfigured,
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidProvisioner",
			Message:            err.Error(),
			ObservedGeneration: config.Generation,
		}) {
			log.Error(err, "Invalid StorageProvisioner")
			return ctrl.Result{}, r.Client.Status().Update(ctx, config)
		}
		return ctrl.Result{}, nil
	}

	if utils.SetCondition(&config.Status.Conditions, metav1.Condition{
		Type:               storageapi.ConditionConfigured,
		Status:             metav1.ConditionTrue,
		Reason:             "Success",
		Message:            "provisioner configured",
		ObservedGeneration: config.Generation,
	}) {
		log.Info("Configured StorageProvisioner", "provisioner", config.Spec.Name)
		return ctrl.Result{}, r.Client.Status().Update(ctx, config)
	}

	return ctrl.Result{}, nil
}
