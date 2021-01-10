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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CacheSpec defines the desired state of Cache
type CacheSpec struct {
	SquashLayers uint `json:"squashLayers,omitempty"`
}

// CacheStatus defines the observed state of Cache
type CacheStatus struct {
	LatestImageID string       `json:"latestImageID,omitempty"`
	LastUsed      metav1.Time  `json:"lastUsed,omitempty"`
	LastWritten   metav1.Time  `json:"lastWritten,omitempty"`
	Nodes         []NodeStatus `json:"nodes,omitempty"`
}

// NodeStatus defines the observed state of a cache on a node
type NodeStatus struct {
	Name          string         `json:"name"`
	LastUsed      metav1.Time    `json:"lastUsed"`
	LatestImageID string         `json:"latestImageID,omitempty"`
	Volumes       []VolumeStatus `json:"volumes,omitempty"`
}

// VolumeStatus defines the observed state of a volume or rather cache mount/umount/commit lifecycle
type VolumeStatus struct {
	Name    string      `json:"name"`
	Phase   string      `json:"phase"`
	Error   string      `json:"message"`
	Created metav1.Time `json:"created"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Cache is the Schema for the caches API
type Cache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CacheSpec   `json:"spec,omitempty"`
	Status CacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CacheList contains a list of Cache
type CacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cache{}, &CacheList{})
}
