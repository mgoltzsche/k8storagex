package utils

import (
	"fmt"
	"sort"

	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
	"github.com/mgoltzsche/k8storagex/internal/template"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type PodSource struct {
	PodName                types.NamespacedName
	ContainerName          string
	SubstitutedProvisioner *storageapi.StorageProvisioner
	Container              *storageapi.ProvisionerContainer
	Env                    []corev1.EnvVar
}

type ProvisionerParams struct {
	NodeName              string
	NodePath              string
	PersistentVolumeName  string
	PersistentVolumeClaim types.NamespacedName
}

func SubstituteProvisionerPlaceholders(tpl *storageapi.StorageProvisioner, p ProvisionerParams) error {
	values := template.NewSubstitution("provisioner template", map[string]string{
		"STORAGE_NODE_NAME":     p.NodeName,
		"STORAGE_NODE_PATH":     p.NodePath,
		"STORAGE_PV_NAME":       p.PersistentVolumeName,
		"STORAGE_PVC_NAME":      p.PersistentVolumeClaim.Name,
		"STORAGE_PVC_NAMESPACE": p.PersistentVolumeClaim.Namespace,
	})
	err := Substitute(tpl, values)
	return errors.Wrapf(err, "invalid provisioner template %s", tpl.Name)
}

func NewProvisionerPod(src PodSource) (*corev1.Pod, error) {
	/*parentHostDir := filepath.Clean(filepath.Dir(src.VolumeHostPath))
	if parentHostDir == "" || parentHostDir == "." || parentHostDir == "/" {
		return nil, fmt.Errorf("invalid volume path %q, must be absolute and parent dir must not be /", src.VolumeHostPath)
	}*/

	containerName := "main"
	container := src.Container
	p := &corev1.Pod{}
	p.Name = src.PodName.Name
	p.Namespace = src.PodName.Namespace
	p.Annotations = map[string]string{}
	p.Spec = src.SubstitutedProvisioner.Spec.PodTemplate
	//p.Spec.NodeName = src.NodeName
	c := findContainer(p.Spec.Containers, containerName)
	c.Env = uniqueEnv(append(append(c.Env, container.Env...), src.Env...))
	if container.Command != nil {
		c.Command = container.Command
	}
	if c.Image == "" {
		return nil, fmt.Errorf("provisioner %s pod template does not specify an image for the %s container", src.SubstitutedProvisioner.Name, containerName)
	}
	if len(c.Command) == 0 {
		return nil, fmt.Errorf("provisioner %s pod template does not specify a command for the %s container", src.SubstitutedProvisioner.Name, containerName)
	}
	if len(c.Args) > 0 {
		return nil, fmt.Errorf("provisioner %s pod template specifies args for the %s container", src.SubstitutedProvisioner.Name, containerName)
	}
	return p, nil
}

type ContainerParams struct {
	Command []string
	Env     []corev1.EnvVar
	Volume  Volume
}

type Volume struct {
	Name      string
	MountPath string
	Source    corev1.VolumeSource
}

func newPodFromTemplate(name types.NamespacedName, tpl corev1.PodSpec, params ContainerParams) *corev1.Pod {
	p := &corev1.Pod{}
	p.Name = name.Name
	p.Namespace = name.Namespace
	p.Spec = tpl
	c := findContainer(p.Spec.Containers, "main")
	c.Command = params.Command
	c.Env = uniqueEnv(append(c.Env, params.Env...))
	c.Args = nil
	addVolume(&p.Spec.Volumes, params.Volume)
	m := addVolumeMount(&c.VolumeMounts, params.Volume)
	c.Env = []corev1.EnvVar{{Name: "VOLUME_PATH", Value: m.MountPath}}
	return p
}

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i, c := range containers {
		if c.Name == name {
			return &containers[i]
		}
	}
	return nil
}

func uniqueEnv(env []corev1.EnvVar) []corev1.EnvVar {
	m := make(map[string]corev1.EnvVar, len(env))
	for _, v := range env {
		m[v.Name] = v
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	filtered := make([]corev1.EnvVar, len(m))
	for i, name := range names {
		filtered[i] = m[name]
	}
	return filtered
}

func addVolumeMount(mounts *[]corev1.VolumeMount, mount Volume) *corev1.VolumeMount {
	for i, m := range *mounts {
		if m.Name == mount.Name {
			if m.MountPath == "" || m.MountPath == "/replace-with-host-path" {
				(*mounts)[i].MountPath = mount.MountPath
			}
			return &(*mounts)[i]
		}
	}
	*mounts = append(*mounts, corev1.VolumeMount{Name: mount.Name, MountPath: mount.MountPath})
	return &(*mounts)[len(*mounts)-1]
}

func addVolume(vols *[]corev1.Volume, vol Volume) {
	for _, v := range *vols {
		if v.Name == vol.Name {
			return // exists already
		}
	}
	*vols = append(*vols, corev1.Volume{Name: vol.Name, VolumeSource: vol.Source})
}
