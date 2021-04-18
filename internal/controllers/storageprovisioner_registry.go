package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/go-logr/logr"
	storageapi "github.com/mgoltzsche/cache-provisioner/api/v1alpha1"
	"github.com/mgoltzsche/cache-provisioner/internal/utils"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Provisioners interface {
	Get(name string) *storageapi.StorageProvisioner
}

type ProvisionerRegistry struct {
	mutex        *sync.Mutex
	provisioners map[string]*storageapi.StorageProvisioner
}

func LoadProvisioners(ctx context.Context, c client.Client, namespace string, log logr.Logger) (*ProvisionerRegistry, error) {
	list := storageapi.StorageProvisionerList{}
	err := c.List(ctx, &list, &client.ListOptions{Namespace: namespace})
	if err != nil {
		return nil, err
	}
	r := newProvisioners()
	for _, p := range list.Items {
		if err = r.Put(&p); err != nil {
			log.Error(err, "Invalid provisioner", "storageprovisioner", p.GetName())
		}
	}
	return r, nil
}

func newProvisioners() *ProvisionerRegistry {
	return &ProvisionerRegistry{
		mutex:        &sync.Mutex{},
		provisioners: map[string]*storageapi.StorageProvisioner{},
	}
}

func (r *ProvisionerRegistry) Keys() []string {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	keys := make([]string, 0, len(r.provisioners))
	for k, v := range r.provisioners {
		if v != nil {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func (r *ProvisionerRegistry) Put(p *storageapi.StorageProvisioner) error {
	err := validateProvisioner(p)
	if err != nil {
		return errors.Wrapf(err, "validate StorageProvisioner %s", p.Name)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	key := p.GetProvisionerName()
	existing, ok := r.provisioners[key]
	if ok && (existing == nil || p.Name != existing.Name || p.Namespace != existing.Namespace) {
		// TODO: Make this consistent
		r.provisioners[key] = nil
		return errors.Errorf("duplicate provisioner %s - key is disabled now", key)
	}
	r.provisioners[key] = p.DeepCopy()
	return nil
}

func (r *ProvisionerRegistry) Get(name string) *storageapi.StorageProvisioner {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if p := r.provisioners[name]; p != nil {
		return p.DeepCopy()
	}
	return nil
}

func (r *ProvisionerRegistry) Delete(p *storageapi.StorageProvisioner) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	key := p.GetProvisionerName()
	delete(r.provisioners, key)
}

func validateProvisioner(p *storageapi.StorageProvisioner) error {
	p = p.DeepCopy()
	utils.SubstituteProvisionerPlaceholders(p, utils.ProvisionerParams{
		NodeName:              "test-node",
		NodePath:              "/test-node-path",
		PersistentVolumeName:  "test-pv",
		PersistentVolumeClaim: types.NamespacedName{Name: "test-pvc", Namespace: "test-pvc-ns"},
	})
	_, err := utils.NewProvisionerPod(utils.PodSource{
		ContainerName: provisioner,
		PodName: types.NamespacedName{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
		SubstitutedProvisioner: p,
		Container:              &p.Spec.Containers.Provisioner,
	})
	if err != nil {
		return err
	}
	if p.Spec.PersistentVolumeTemplate.VolumeMode == nil {
		return fmt.Errorf("spec.persistentVolumeTemplate.VolumeMode is empty")
	}
	for i, env := range p.Spec.Env {
		if env.Name == "" {
			return fmt.Errorf("spec.env[%d].name is empty", i)
		}
		if env.Annotation == "" {
			return fmt.Errorf("spec.env[%d].annotation is empty", i)
		}
	}
	if len(p.Spec.Nodes) == 0 {
		return fmt.Errorf("spec.nodes is empty")
	}
	for i, node := range p.Spec.Nodes {
		if node.Name == "" {
			return fmt.Errorf("spec.nodes[%d].name is empty", i)
		}
		if _, err = filepath.Match(node.Name, "test-node"); err != nil {
			return fmt.Errorf("spec.nodes[%d].name is an invalid matcher: %w", i, err)
		}
		path := filepath.Clean(node.Path)
		if filepath.Dir(path) == path || !filepath.IsAbs(path) {
			return fmt.Errorf("spec.nodes[%d].path must be an absolute sub directory but is %q", i, path)
		}
	}
	// TODO: validate other containers, missing fields etc
	return nil
}
