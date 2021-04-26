package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	mountCmd = &cobra.Command{
		Use:     "mount",
		Short:   "mount a cache image to a directory",
		Long:    "Mount a cache image to a directory",
		Example: fmt.Sprintf("  %s mount --name mycache /data/myvolume", os.Args[0]),
		Args:    cobra.RangeArgs(0, 1),
		PreRunE: validateOptions,
		RunE:    runMountCmd,
	}
	modeFlag os.FileMode = 0750
)

func init() {
	mountCmd.Flags().Uint32Var((*uint32)(&modeFlag), "mode", uint32(modeFlag), "set mount directory access permissions")
	addContainerFlag(mountCmd)
	rootCmd.AddCommand(mountCmd)
}

func runMountCmd(cmd *cobra.Command, args []string) (err error) {
	if len(args) > 0 {
		mountOptions.ExtMountDir = args[0]
	}
	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Free()
	applyDefaults(&mountOptions)
	dir, err := store.Mount(mountOptions)
	if err != nil {
		return err
	}
	err = os.Chmod(dir, modeFlag)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), dir)
	return err
}
