package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mountCmd = &cobra.Command{
	Use:   "mount",
	Short: "mount a cache image to a directory",
	Long:  "Mount a cache image to a directory",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runMountCmd,
}

func init() {
	addContainerNameFlag(mountCmd)
	rootCmd.AddCommand(mountCmd)
}

func runMountCmd(cmd *cobra.Command, args []string) (err error) {
	mountOptions.Image = args[0]
	if len(args) > 1 {
		mountOptions.ExtMountDir = args[1]
	}
	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Free()
	dir, err := store.Mount(mountOptions)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), dir)
	return err
}
