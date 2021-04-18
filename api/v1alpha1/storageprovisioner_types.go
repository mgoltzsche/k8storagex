/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionConfigured = "Configured"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageProvisionerSpec defines the desired state of StorageProvisioner
type StorageProvisionerSpec struct {
	Name                     string                      `json:"name"`
	PersistentVolumeTemplate corev1.PersistentVolumeSpec `json:"persistentVolumeTemplate"`
	PodTemplate              corev1.PodSpec              `json:"podTemplate"`
	Containers               Containers                  `json:"containers"`
	Env                      []EnvVar                    `json:"env,omitempty"`
	Nodes                    []NodePath                  `json:"nodes,omitempty"`
}

// EnvVar maps an annotation value to an env var that is provided to the de/provisioner Pod
type EnvVar struct {
	Name       string `json:"name"`
	Annotation string `json:"annotation"`
	Required   *bool  `json:"required,omitempty"`
}

// NodePath maps nodes to a path
type NodePath struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path"`
}

// Containers specifies the parameters used with the PodTemplate to create the Pods to handle the storage lifecycle
type Containers struct {
	Provisioner   ProvisionerContainer `json:"provisioner"`
	Deprovisioner ProvisionerContainer `json:"deprovisioner"`
}

// ProvisionerContainer specifies a container that is merged with a Pod template
type ProvisionerContainer struct {
	Command []string        `json:"command,omitempty"`
	Env     []corev1.EnvVar `json:"env,omitempty"`
}

// StorageProvisionerStatus defines the observed state of StorageProvisioner
type StorageProvisionerStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// StorageProvisioner is the Schema for the storageprovisioners API
type StorageProvisioner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageProvisionerSpec   `json:"spec,omitempty"`
	Status StorageProvisionerStatus `json:"status,omitempty"`
}

func (p *StorageProvisioner) GetProvisionerName() string {
	return p.Spec.Name
}

// +kubebuilder:object:root=true

// StorageProvisionerList contains a list of StorageProvisioner
type StorageProvisionerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageProvisioner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageProvisioner{}, &StorageProvisionerList{})
}
