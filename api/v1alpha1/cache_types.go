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

const (
	CachePhaseReady       CachePhase = "ready"
	CachePhaseReject      CachePhase = "reject"
	VolumePhaseMount                 = "mount"
	VolumePhaseCommit                = "commit"
	CacheFinalizer                   = "cache-provisioner.mgoltzsche.github.com/finalizer"
	ConditionStorageReset            = "StorageReset"
	ConditionPodsCleared             = "PodsCleared"
)

type VolumePhase string

type CachePhase string

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CacheSpec defines the desired state of Cache
type CacheSpec struct {
	//BaseCacheName string `json:"baseCacheName,omitempty"`
	ReadOnly bool `json:"readOnly,omitempty"`
}

// CacheStatus defines the observed state of Cache
type CacheStatus struct {
	Image           string       `json:"image,omitempty"`
	CacheGeneration int64        `json:"cacheGeneration,omitempty"`
	LastImageID     *string      `json:"lastImageID,omitempty"`
	LastUsed        *metav1.Time `json:"lastUsed,omitempty"`
	LastWritten     *metav1.Time `json:"lastWritten,omitempty"`
	LastReset       *ResetStatus `json:"lastReset,omitempty"`
	Used            int64        `json:"used"`
	//LastWrittenByPersistentVolumeClaim string       `json:"lastWrittenByPersistentVolumeClaim,omitempty"`
	Nodes []NodeStatus `json:"nodes,omitempty"`
	Phase CachePhase   `json:"phase,omitempty"`
	// Conditions represent the latest available observations of a Cache's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NodeStatus defines the observed state of a cache on a node
type NodeStatus struct {
	Name            string         `json:"name"`
	CacheGeneration int64          `json:"cacheGeneration,omitempty"`
	LastUsed        metav1.Time    `json:"lastUsed"`
	LastImageID     string         `json:"lastImageID,omitempty"`
	Volumes         []VolumeStatus `json:"volumes,omitempty"`
	LastError       *VolumeError   `json:"lastError,omitempty"`
}

type VolumeError struct {
	VolumeName      string      `json:"volumeName"`
	CacheGeneration *int64      `json:"cacheGeneration,omitempty"`
	Error           string      `json:"error"`
	Happened        metav1.Time `json:"happened"`
}

// VolumeStatus defines the observed state of a volume or rather cache mount/umount/commit lifecycle
type VolumeStatus struct {
	Name            string       `json:"name"`
	CacheGeneration int64        `json:"cacheGeneration"`
	Created         metav1.Time  `json:"created"`
	Committable     bool         `json:"committable,omitempty"`
	CommitStartTime *metav1.Time `json:"commitStartTime,omitempty"`
}

type ResetStatus struct {
	CacheGeneration int64       `json:"cacheGeneration"`
	ResetTime       metav1.Time `json:"resetTime"`
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
