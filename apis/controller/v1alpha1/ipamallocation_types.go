/*
 * 	Copyright (c) 2022 Avesha, Inc. All rights reserved. # # SPDX-License-Identifier: Apache-2.0
 *
 * 	Licensed under the Apache License, Version 2.0 (the "License");
 * 	you may not use this file except in compliance with the License.
 * 	You may obtain a copy of the License at
 *
 * 	http://www.apache.org/licenses/LICENSE-2.0
 *
 * 	Unless required by applicable law or agreed to in writing, software
 * 	distributed under the License is distributed on an "AS IS" BASIS,
 * 	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * 	See the License for the specific language governing permissions and
 * 	limitations under the License.
 */

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPAMAllocationSpec defines the desired state of IPAMAllocation
type IPAMAllocationSpec struct {
	// SliceName is the name of the slice for which IP allocation is being tracked
	SliceName string `json:"sliceName"`
	// SliceSubnet is the base subnet CIDR for the slice (e.g., 10.1.0.0/16)
	SliceSubnet string `json:"sliceSubnet"`
	// ClusterAllocations tracks IP allocations per cluster
	ClusterAllocations []ClusterIPAllocation `json:"clusterAllocations,omitempty"`
}

// ClusterIPAllocation defines IP allocation for a specific cluster
type ClusterIPAllocation struct {
	// ClusterName is the name of the cluster
	ClusterName string `json:"clusterName"`
	// SubnetCIDR is the allocated subnet for this cluster (e.g., 10.1.1.0/24)
	SubnetCIDR string `json:"subnetCIDR"`
	// AllocatedAt is the timestamp when the allocation was made
	AllocatedAt metav1.Time `json:"allocatedAt"`
	// Status indicates the current status of the allocation
	//+kubebuilder:validation:Enum:=Allocated;Released;Pending
	Status AllocationStatus `json:"status"`
}

// AllocationStatus represents the status of an IP allocation
type AllocationStatus string

const (
	// AllocationStatusAllocated indicates the subnet is allocated and in use
	AllocationStatusAllocated AllocationStatus = "Allocated"
	// AllocationStatusReleased indicates the subnet has been released and can be reused
	AllocationStatusReleased AllocationStatus = "Released"
	// AllocationStatusPending indicates the allocation is pending
	AllocationStatusPending AllocationStatus = "Pending"
)

// IPAMAllocationStatus defines the observed state of IPAMAllocation
type IPAMAllocationStatus struct {
	// AllocatedSubnets tracks the number of currently allocated subnets
	AllocatedSubnets int `json:"allocatedSubnets,omitempty"`
	// AvailableSubnets tracks the number of available subnets in the slice
	AvailableSubnets int `json:"availableSubnets,omitempty"`
	// LastUpdated is the timestamp of the last status update
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// IPAMAllocation is the Schema for the ipamallocation API
type IPAMAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPAMAllocationSpec   `json:"spec,omitempty"`
	Status IPAMAllocationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IPAMAllocationList contains a list of IPAMAllocation
type IPAMAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPAMAllocation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPAMAllocation{}, &IPAMAllocationList{})
}
