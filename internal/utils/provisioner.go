package utils

import (
	"encoding/json"

	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
	"github.com/pkg/errors"
)

func StorageProvisionerFromJSON(data string) (*storageapi.StorageProvisioner, error) {
	p := &storageapi.StorageProvisioner{}
	err := json.Unmarshal([]byte(data), p)
	return p, errors.Wrap(err, "unmarshal provisioner spec")
}

func StorageProvisionerToJSON(provisioner *storageapi.StorageProvisioner) (string, error) {
	p := &storageapi.StorageProvisioner{}
	p.Name = provisioner.Name
	p.Namespace = provisioner.Namespace
	p.UID = provisioner.UID
	p.Generation = provisioner.Generation
	p.Spec = provisioner.Spec
	b, err := json.Marshal(p)
	return string(b), errors.Wrap(err, "marshal provisioner spec")
}
