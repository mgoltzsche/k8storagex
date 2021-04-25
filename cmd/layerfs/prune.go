package main

import (
	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "prune storage",
	Long:  "Delete all unused images",
	Args:  cobra.ExactArgs(0),
	RunE:  runPruneCmd,
}

func init() {
	rootCmd.AddCommand(pruneCmd)
}

func runPruneCmd(cmd *cobra.Command, args []string) (err error) {
	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Free()
	return store.Prune(newContext())
}
