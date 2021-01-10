package main

import (
	"fmt"
	"io"
	"os"

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
	envStorageRoot     = "KUBE_CACHE_STORAGE_ROOT"
	envStorageRunRoot  = "KUBE_CACHE_STORAGE_RUNROOT"
	envRegistry        = "KUBE_CACHE_STORAGE_REGISTRY"
	debugFlag          bool
	storageRootFlag    = os.Getenv(envStorageRoot)
	storageRunRootFlag = os.Getenv(envStorageRunRoot)
	registryFlag       = os.Getenv(envRegistry)
)

// Execute runs the CLI
func Execute(out io.Writer) error {
	rootCmd.SetFlagErrorFunc(handleFlagError)
	rootCmd.SetOut(os.Stdout)
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", debugFlag, "enables debug log")
	rootCmd.PersistentFlags().StringVar(&storageRootFlag, "storage-root", storageRootFlag, fmt.Sprintf("sets the storage root directory (%s)", envStorageRoot))
	rootCmd.PersistentFlags().StringVar(&storageRunRootFlag, "storage-runroot", storageRunRootFlag, fmt.Sprintf("sets the storage state directory (%s)", envStorageRunRoot))
	rootCmd.PersistentFlags().StringVar(&registryFlag, "registry", registryFlag, fmt.Sprintf("sets the registry (%s)", envRegistry))
	return rootCmd.Execute()
}

func handleFlagError(cmd *cobra.Command, err error) error {
	cmd.Help()
	return err
}
