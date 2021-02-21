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
		Use:           "dcowfs",
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
	envStorageRoot       = "DCOWFS_STORAGE_ROOT"
	envStorageRunRoot    = "DCOWFS_STORAGE_RUNROOT"
	envRegistry          = "DCOWFS_REGISTRY"
	envRegistryUsername  = "DCOWFS_REGISTRY_USERNAME"
	envRegistryPassword  = "DCOWFS_REGISTRY_PASSWORD"
	envInsecure          = "DCOWFS_INSECURE_SKIP_TLS_VERIFY"
	envEnableK8sSync     = "DCOWFS_ENABLE_K8S_SYNC"
	envNodeName          = "DCOWFS_NODE_NAME"
	envCacheName         = "DCOWFS_NAME"
	envCacheNamespace    = "DCOWFS_NAMESPACE"
	envCacheImage        = "DCOWFS_IMAGE"
	envContainerName     = "DCOWFS_CONTAINER_NAME"
	debugFlag            bool
	storageRootFlag      = os.Getenv(envStorageRoot)
	storageRunRootFlag   = os.Getenv(envStorageRunRoot)
	registryFlag         = os.Getenv(envRegistry)
	registryUsernameFlag = os.Getenv(envRegistryUsername)
	registryPasswordFlag = os.Getenv(envRegistryPassword)
	insecureFlag, _      = strconv.ParseBool(os.Getenv(envInsecure))
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
	f.StringVar(&registryUsernameFlag, "registry-username", registryUsernameFlag, fmt.Sprintf("cache image registry username (%s)", envRegistryUsername))
	f.StringVar(&registryPasswordFlag, "registry-password", registryPasswordFlag, fmt.Sprintf("cache image registry password (%s)", envRegistryPassword))
	f.BoolVar(&insecureFlag, "insecure-skip-tls-verify", insecureFlag, fmt.Sprintf("skips registry TLS certificate verification - do not enable in production (%s)", envInsecure))
	f.BoolVar(&enableK8sSyncFlag, "enable-k8s-sync", enableK8sSyncFlag, "synchronizes cache operations with a Kubernetes Cache resource")
	return rootCmd.Execute()
}

func handleFlagError(cmd *cobra.Command, err error) error {
	cmd.Help()
	return err
}
