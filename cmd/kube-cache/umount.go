package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	umountCmd = &cobra.Command{
		Use:   "umount",
		Short: "unmount (and commit) a cache directory",
		Long:  "Unmount a cache directory and optionally publish it as new image",
		Args:  cobra.RangeArgs(0, 1),
		RunE:  runUnmountCmd,
	}
)

func init() {
	addContainerFlag(umountCmd)
	rootCmd.AddCommand(umountCmd)
}

func runUnmountCmd(cmd *cobra.Command, args []string) (err error) {
	if len(args) > 0 {
		mountOptions.ExtMountDir = args[0]
	}
	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Free()
	applyDefaults(&mountOptions)
	imageID, _, err := store.Unmount(mountOptions)
	if err != nil {
		return err
	}
	if imageID != "" {
		fmt.Fprintln(cmd.OutOrStdout(), imageID)
	}
	return nil
}
