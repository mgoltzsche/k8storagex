package utils

import (
	"fmt"
	"path/filepath"

	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
)

func StorageRootPathForNode(nodeName string, mapping []storageapi.NodePath) (string, error) {
	for i, m := range mapping {
		match, err := filepath.Match(m.Name, nodeName)
		if err != nil {
			return "", fmt.Errorf("invalid node name matcher specified in spec.nodes[%d].name", i)
		}
		if match {
			return m.Path, nil
		}
	}
	return "", fmt.Errorf("no storage root path mapped for node %q", nodeName)
}
