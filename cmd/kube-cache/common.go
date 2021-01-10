package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	imgstorage "github.com/containers/image/v5/storage"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/unshare"
	"github.com/mgoltzsche/cache-provisioner/internal/cache"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	_ "github.com/containers/storage/drivers/overlay"
)

var (
	mountOptions = cache.CacheMountOptions{Context: newContext()}
)

func addContainerNameFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&mountOptions.ContainerName, "name", "", "sets the name of the cache container (otherwise derived from --mount)")
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

func newStore() (*cache.Store, error) {
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
	return cache.New(store), nil
}
