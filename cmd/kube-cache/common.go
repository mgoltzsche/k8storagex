package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"io/ioutil"

	imgstorage "github.com/containers/image/v5/storage"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/unshare"
	cacheapi "github.com/mgoltzsche/cache-provisioner/api/v1alpha1"
	"github.com/mgoltzsche/cache-provisioner/internal/cache"
	"github.com/mgoltzsche/cache-provisioner/internal/ksync"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	_ "github.com/containers/storage/drivers/overlay"
)

var (
	mountOptions = cache.CacheMountOptions{
		Context:        newContext(),
		CacheName:      os.Getenv(envCacheName),
		CacheNamespace: os.Getenv(envCacheNamespace),
		ContainerName:  os.Getenv(envContainerName),
	}
)

func addContainerFlag(cmd *cobra.Command) {
	if mountOptions.CacheNamespace == "" {
		b, _ := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		mountOptions.CacheNamespace = string(b)
	}

	f := cmd.Flags()
	f.BoolVar(&mountOptions.Commit, "commit", false, "commits the container to a new image if contents changed")
	f.StringVar(&mountOptions.Image, "image", mountOptions.Image, "sets the cache image name")
	f.StringVar(&mountOptions.ContainerName, "container-name", mountOptions.ContainerName, "sets the name of the cache container (otherwise derived from mount path arg)")
	f.StringVar(&mountOptions.CacheName, "name", mountOptions.CacheName, "sets the cache's name")
	f.StringVar(&mountOptions.CacheNamespace, "namespace", mountOptions.CacheNamespace, "sets the cache's namespace")
}

func validateOptions(cmd *cobra.Command, _ []string) error {
	if mountOptions.Image == "" && mountOptions.CacheName == "" {
		return fmt.Errorf("neither --cache-name nor --image specified")
	}
	return nil
}

func newContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logrus.Infof("Received %v signal - terminating", sig)
		cancel()
		<-sigs
		logrus.Info("Received 2nd termination signal - exiting forcefully")
		os.Exit(254)
	}()
	return ctx
}

func newStore() (r cache.Store, err error) {
	opts, err := storage.DefaultStoreOptions(unshare.IsRootless(), unshare.GetRootlessUID())
	if err != nil {
		return nil, err
	}
	if storageRootFlag != "" && storageRootFlag != opts.GraphRoot {
		opts.GraphRoot = storageRootFlag
		opts.RunRoot = filepath.Join(opts.GraphRoot, "runroot")
	}
	if storageRunRootFlag != "" {
		opts.RunRoot = storageRunRootFlag
	}
	if opts.GraphDriverName == "" {
		opts.GraphDriverName = "overlay"
		opts.GraphDriverOptions = []string{"overlay.mountopt=nodev"}
	}
	logrus.Debugf("Root: %s", opts.GraphRoot)
	logrus.Debugf("Run Root: %s", opts.RunRoot)
	logrus.Debugf("Driver Name: %s", opts.GraphDriverName)
	logrus.Debugf("Driver Options: %s", opts.GraphDriverOptions)
	store, err := storage.GetStore(opts)
	if err != nil {
		return nil, fmt.Errorf("init store at %s: %w", opts.GraphRoot, err)
	}
	imgstorage.Transport.SetStore(store)
	r = cache.New(store, logrus.NewEntry(logrus.StandardLogger()))
	if enableK8sSyncFlag {
		r, err = toClusterSyncedStore(r)
		if err != nil {
			return nil, fmt.Errorf("cannot enable k8s sync: %w", err)
		}
	}
	return r, nil
}

func toClusterSyncedStore(store cache.Store) (cache.Store, error) {
	if nodeNameFlag == "" {
		return nil, fmt.Errorf("node name has not been specified")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("needs to be run within a k8s pod with an access token mounted: %w", err)
	}
	scheme := runtime.NewScheme()
	cacheapi.AddToScheme(scheme)
	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		return nil, err
	}
	c, err := client.New(config, client.Options{Scheme: scheme, Mapper: mapper})
	if err != nil {
		return nil, err
	}
	return ksync.Synchronized(store, c, nodeNameFlag), nil
}
