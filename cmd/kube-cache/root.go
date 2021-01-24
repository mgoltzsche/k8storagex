package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:           "kube-cache",
		Short:         "A distributed, layered, container storage based cache",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if debugFlag {
				logrus.SetLevel(logrus.DebugLevel)
			} else {
				logrus.SetLevel(logrus.InfoLevel)
			}
		},
	}
	envStorageRoot       = "KUBE_CACHE_STORAGE_ROOT"
	envStorageRunRoot    = "KUBE_CACHE_STORAGE_RUNROOT"
	envRegistry          = "KUBE_CACHE_STORAGE_REGISTRY"
	envEnableK8sSync     = "KUBE_CACHE_ENABLE_K8S_SYNC"
	envNodeName          = "KUBE_CACHE_NODE_NAME"
	envCacheName         = "KUBE_CACHE_NAME"
	envCacheNamespace    = "KUBE_CACHE_NAMESPACE"
	envContainerName     = "KUBE_CACHE_CONTAINER_NAME"
	debugFlag            bool
	storageRootFlag      = os.Getenv(envStorageRoot)
	storageRunRootFlag   = os.Getenv(envStorageRunRoot)
	registryFlag         = os.Getenv(envRegistry)
	enableK8sSyncFlag, _ = strconv.ParseBool(os.Getenv(envEnableK8sSync))
	nodeNameFlag         = os.Getenv(envNodeName)
)

// Execute runs the CLI
func Execute(out io.Writer) error {
	rootCmd.SetFlagErrorFunc(handleFlagError)
	rootCmd.SetOut(os.Stdout)
	f := rootCmd.PersistentFlags()
	f.BoolVar(&debugFlag, "debug", debugFlag, "enables debug log")
	f.StringVar(&storageRootFlag, "storage-root", storageRootFlag, fmt.Sprintf("sets the storage root directory (%s)", envStorageRoot))
	f.StringVar(&storageRunRootFlag, "storage-runroot", storageRunRootFlag, fmt.Sprintf("sets the storage state directory (%s)", envStorageRunRoot))
	f.StringVar(&registryFlag, "registry", registryFlag, fmt.Sprintf("sets the registry (%s)", envRegistry))
	f.BoolVar(&enableK8sSyncFlag, "enable-k8s-sync", enableK8sSyncFlag, "synchronizes cache operations with a Kubernetes Cache resource")
	return rootCmd.Execute()
}

func handleFlagError(cmd *cobra.Command, err error) error {
	cmd.Help()
	return err
}
