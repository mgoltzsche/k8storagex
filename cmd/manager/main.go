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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
	"github.com/mgoltzsche/k8storagex/internal/controllers"
	// +kubebuilder:scaffold:imports
)

const (
	envManagerNamespace          = "MANAGER_NAMESPACE"
	envProvisioner               = "PROVISIONER"
	envProvisionerImage          = "PROVISIONER_IMAGE"
	envProvisionerServiceAccount = "PROVISIONER_SERVICE_ACCOUNT"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(storageapi.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		managerNamespace     = os.Getenv(envManagerNamespace)
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&managerNamespace, "manager-namespace", managerNamespace, "The namespace provisioner Pods are run in")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if managerNamespace == "" {
		setupLog.Error(fmt.Errorf("no --manager-namespace specified"), "invalid usage")
		os.Exit(1)
	}

	config := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "146685bb.cache-manager",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	provisioners, err := loadProvisioners(ctx, config, mgr.GetRESTMapper(), managerNamespace, setupLog)
	if err != nil {
		setupLog.Error(err, "unable to load provisioners")
		os.Exit(1)
	}

	// TODO: implement Cache reconciler properly
	/*if err = (&controllers.CacheReconciler{
		Client:             mgr.GetClient(),
		Log:                ctrl.Log.WithName("controllers").WithName("Cache"),
		Scheme:             mgr.GetScheme(),
		ManagerNamespace:   managerNamespace,
		ServiceAccountName: provisionerServiceAccount,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cache")
		os.Exit(1)
	}*/
	if err = (&controllers.PersistentVolumeClaimReconciler{
		Client:           mgr.GetClient(),
		Log:              ctrl.Log.WithName("controllers").WithName("PersistentVolumeClaim"),
		Scheme:           mgr.GetScheme(),
		ManagerNamespace: managerNamespace,
		Provisioners:     provisioners,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PersistentVolumeClaim")
		os.Exit(1)
	}
	if err = (&controllers.PersistentVolumeReconciler{
		Client:           mgr.GetClient(),
		Log:              ctrl.Log.WithName("controllers").WithName("PersistentVolume"),
		Scheme:           mgr.GetScheme(),
		ManagerNamespace: managerNamespace,
		Provisioners:     provisioners,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PersistentVolume")
		os.Exit(1)
	}
	if err = (&controllers.StorageProvisionerReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("StorageProvisioner"),
		Scheme:       mgr.GetScheme(),
		Provisioners: provisioners,
		Namespace:    managerNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StorageProvisioner")
		os.Exit(1)
	}
	err = (&controllers.PodReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("Pod"),
		Scheme:       mgr.GetScheme(),
		Provisioners: provisioners,
	}).SetupWithManager(mgr)
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func loadProvisioners(ctx context.Context, cfg *rest.Config, mapper meta.RESTMapper, namespace string, log logr.Logger) (*controllers.ProvisionerRegistry, error) {
	client, err := client.New(cfg, client.Options{Scheme: scheme, Mapper: mapper})
	if err != nil {
		return nil, err
	}
	provisioners, err := controllers.LoadProvisioners(ctx, client, namespace, log)
	if err != nil {
		return nil, err
	}
	provisionerKeys := provisioners.Keys()
	if len(provisionerKeys) > 0 {
		log.Info(fmt.Sprintf("Configured provisioners %s", strings.Join(provisionerKeys, ", ")))
	} else {
		log.Info("No provisioners configured. Please create StorageProvisioner resources within the operator namespace")
	}
	return provisioners, nil
}
