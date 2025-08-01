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

package service

import (
	"context"
	"fmt"
	"net"

	"github.com/kubeslice/kubeslice-controller/apis/controller/v1alpha1"
	"github.com/kubeslice/kubeslice-controller/events"
	"github.com/kubeslice/kubeslice-controller/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// IDynamicIPAMService defines the interface for dynamic IPAM operations
type IDynamicIPAMService interface {
	// AllocateSubnetForCluster allocates a subnet for a cluster in a slice
	AllocateSubnetForCluster(ctx context.Context, sliceName, sliceSubnet, clusterName, namespace string) (string, error)
	// DeallocateSubnetForCluster deallocates a subnet for a cluster in a slice
	DeallocateSubnetForCluster(ctx context.Context, sliceName, clusterName, namespace string) error
	// GetClusterSubnet gets the allocated subnet for a cluster in a slice
	GetClusterSubnet(ctx context.Context, sliceName, clusterName, namespace string) (string, error)
	// ReconcileIPAMAllocation reconciles IPAM allocations for a slice
	ReconcileIPAMAllocation(ctx context.Context, sliceName, sliceSubnet, namespace string, clusters []string) error
}

// DynamicIPAMService implements dynamic IPAM functionality
type DynamicIPAMService struct{}

// NewDynamicIPAMService creates a new instance of DynamicIPAMService
func NewDynamicIPAMService() IDynamicIPAMService {
	return &DynamicIPAMService{}
}

// AllocateSubnetForCluster allocates a subnet for a cluster in a slice
func (d *DynamicIPAMService) AllocateSubnetForCluster(ctx context.Context, sliceName, sliceSubnet, clusterName, namespace string) (string, error) {
	logger := util.CtxLogger(ctx)
	logger.Infof("Allocating subnet for cluster %s in slice %s", clusterName, sliceName)

	// Get or create IPAM allocation resource
	ipamAllocation, err := d.getOrCreateIPAMAllocation(ctx, sliceName, sliceSubnet, namespace)
	if err != nil {
		return "", fmt.Errorf("failed to get or create IPAM allocation: %w", err)
	}

	// Check if cluster already has an allocation
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.ClusterName == clusterName && allocation.Status == v1alpha1.AllocationStatusAllocated {
			logger.Infof("Cluster %s already has allocated subnet %s", clusterName, allocation.SubnetCIDR)
			return allocation.SubnetCIDR, nil
		}
	}

	// Find next available subnet
	subnetCIDR, err := d.findNextAvailableSubnet(sliceSubnet, ipamAllocation.Spec.ClusterAllocations)
	if err != nil {
		return "", fmt.Errorf("failed to find available subnet: %w", err)
	}

	// Add new allocation
	newAllocation := v1alpha1.ClusterIPAllocation{
		ClusterName: clusterName,
		SubnetCIDR:  subnetCIDR,
		AllocatedAt: metav1.Now(),
		Status:      v1alpha1.AllocationStatusAllocated,
	}

	// Remove any existing allocation for this cluster (in case it was released)
	filteredAllocations := make([]v1alpha1.ClusterIPAllocation, 0)
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.ClusterName != clusterName {
			filteredAllocations = append(filteredAllocations, allocation)
		}
	}
	filteredAllocations = append(filteredAllocations, newAllocation)
	ipamAllocation.Spec.ClusterAllocations = filteredAllocations

	// Update status
	d.updateIPAMStatus(ipamAllocation)

	// Update the resource
	if err := util.UpdateResource(ctx, ipamAllocation); err != nil {
		return "", fmt.Errorf("failed to update IPAM allocation: %w", err)
	}

	logger.Infof("Allocated subnet %s for cluster %s in slice %s", subnetCIDR, clusterName, sliceName)

	// Record event
	eventRecorder := util.CtxEventRecorder(ctx).
		WithProject(util.GetProjectName(namespace)).
		WithNamespace(namespace).
		WithSlice(sliceName)
	util.RecordEvent(ctx, eventRecorder, ipamAllocation, nil, events.EventWorkerSliceConfigCreated)

	return subnetCIDR, nil
}

// DeallocateSubnetForCluster deallocates a subnet for a cluster in a slice
func (d *DynamicIPAMService) DeallocateSubnetForCluster(ctx context.Context, sliceName, clusterName, namespace string) error {
	logger := util.CtxLogger(ctx)
	logger.Infof("Deallocating subnet for cluster %s in slice %s", clusterName, sliceName)

	// Get IPAM allocation resource
	ipamAllocation := &v1alpha1.IPAMAllocation{}
	found, err := util.GetResourceIfExist(ctx, types.NamespacedName{
		Name:      d.getIPAMAllocationName(sliceName),
		Namespace: namespace,
	}, ipamAllocation)
	if err != nil {
		return fmt.Errorf("failed to get IPAM allocation: %w", err)
	}
	if !found {
		logger.Infof("IPAM allocation not found for slice %s, nothing to deallocate", sliceName)
		return nil
	}

	// Mark allocation as released
	updated := false
	for i, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.ClusterName == clusterName && allocation.Status == v1alpha1.AllocationStatusAllocated {
			ipamAllocation.Spec.ClusterAllocations[i].Status = v1alpha1.AllocationStatusReleased
			updated = true
			logger.Infof("Marked subnet %s as released for cluster %s", allocation.SubnetCIDR, clusterName)
			break
		}
	}

	if !updated {
		logger.Infof("No active allocation found for cluster %s in slice %s", clusterName, sliceName)
		return nil
	}

	// Update status
	d.updateIPAMStatus(ipamAllocation)

	// Update the resource
	if err := util.UpdateResource(ctx, ipamAllocation); err != nil {
		return fmt.Errorf("failed to update IPAM allocation: %w", err)
	}

	logger.Infof("Successfully deallocated subnet for cluster %s in slice %s", clusterName, sliceName)
	return nil
}

// GetClusterSubnet gets the allocated subnet for a cluster in a slice
func (d *DynamicIPAMService) GetClusterSubnet(ctx context.Context, sliceName, clusterName, namespace string) (string, error) {
	// Get IPAM allocation resource
	ipamAllocation := &v1alpha1.IPAMAllocation{}
	found, err := util.GetResourceIfExist(ctx, types.NamespacedName{
		Name:      d.getIPAMAllocationName(sliceName),
		Namespace: namespace,
	}, ipamAllocation)
	if err != nil {
		return "", fmt.Errorf("failed to get IPAM allocation: %w", err)
	}
	if !found {
		return "", fmt.Errorf("IPAM allocation not found for slice %s", sliceName)
	}

	// Find allocation for cluster
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.ClusterName == clusterName && allocation.Status == v1alpha1.AllocationStatusAllocated {
			return allocation.SubnetCIDR, nil
		}
	}

	return "", fmt.Errorf("no allocation found for cluster %s in slice %s", clusterName, sliceName)
}

// ReconcileIPAMAllocation reconciles IPAM allocations for a slice
func (d *DynamicIPAMService) ReconcileIPAMAllocation(ctx context.Context, sliceName, sliceSubnet, namespace string, clusters []string) error {
	logger := util.CtxLogger(ctx)
	logger.Infof("Reconciling IPAM allocation for slice %s with clusters %v", sliceName, clusters)

	// Get or create IPAM allocation resource
	ipamAllocation, err := d.getOrCreateIPAMAllocation(ctx, sliceName, sliceSubnet, namespace)
	if err != nil {
		return fmt.Errorf("failed to get or create IPAM allocation: %w", err)
	}

	// Build current cluster set
	currentClusters := make(map[string]bool)
	for _, cluster := range clusters {
		currentClusters[cluster] = true
	}

	// Build allocated cluster set
	allocatedClusters := make(map[string]bool)
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.Status == v1alpha1.AllocationStatusAllocated {
			allocatedClusters[allocation.ClusterName] = true
		}
	}

	// Deallocate subnets for clusters that are no longer in the slice
	for i, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.Status == v1alpha1.AllocationStatusAllocated && !currentClusters[allocation.ClusterName] {
			ipamAllocation.Spec.ClusterAllocations[i].Status = v1alpha1.AllocationStatusReleased
			logger.Infof("Marked subnet %s as released for removed cluster %s", allocation.SubnetCIDR, allocation.ClusterName)
		}
	}

	// Allocate subnets for new clusters
	for _, cluster := range clusters {
		if !allocatedClusters[cluster] {
			// Find next available subnet
			subnetCIDR, err := d.findNextAvailableSubnet(sliceSubnet, ipamAllocation.Spec.ClusterAllocations)
			if err != nil {
				return fmt.Errorf("failed to find available subnet for cluster %s: %w", cluster, err)
			}

			// Add new allocation
			newAllocation := v1alpha1.ClusterIPAllocation{
				ClusterName: cluster,
				SubnetCIDR:  subnetCIDR,
				AllocatedAt: metav1.Now(),
				Status:      v1alpha1.AllocationStatusAllocated,
			}
			ipamAllocation.Spec.ClusterAllocations = append(ipamAllocation.Spec.ClusterAllocations, newAllocation)
			logger.Infof("Allocated subnet %s for new cluster %s", subnetCIDR, cluster)
		}
	}

	// Update status
	d.updateIPAMStatus(ipamAllocation)

	// Update the resource
	if err := util.UpdateResource(ctx, ipamAllocation); err != nil {
		return fmt.Errorf("failed to update IPAM allocation: %w", err)
	}

	logger.Infof("Successfully reconciled IPAM allocation for slice %s", sliceName)
	return nil
}

// getOrCreateIPAMAllocation gets or creates an IPAM allocation resource
func (d *DynamicIPAMService) getOrCreateIPAMAllocation(ctx context.Context, sliceName, sliceSubnet, namespace string) (*v1alpha1.IPAMAllocation, error) {
	ipamAllocation := &v1alpha1.IPAMAllocation{}
	found, err := util.GetResourceIfExist(ctx, types.NamespacedName{
		Name:      d.getIPAMAllocationName(sliceName),
		Namespace: namespace,
	}, ipamAllocation)
	if err != nil {
		return nil, err
	}

	if found {
		return ipamAllocation, nil
	}

	// Create new IPAM allocation
	ipamAllocation = &v1alpha1.IPAMAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.getIPAMAllocationName(sliceName),
			Namespace: namespace,
			Labels: map[string]string{
				"slice-name": sliceName,
			},
		},
		Spec: v1alpha1.IPAMAllocationSpec{
			SliceName:          sliceName,
			SliceSubnet:        sliceSubnet,
			ClusterAllocations: []v1alpha1.ClusterIPAllocation{},
		},
	}

	if err := util.CreateResource(ctx, ipamAllocation); err != nil {
		return nil, err
	}

	return ipamAllocation, nil
}

// findNextAvailableSubnet finds the next available subnet in the slice
func (d *DynamicIPAMService) findNextAvailableSubnet(sliceSubnet string, allocations []v1alpha1.ClusterIPAllocation) (string, error) {
	// Parse slice subnet
	_, sliceNet, err := net.ParseCIDR(sliceSubnet)
	if err != nil {
		return "", fmt.Errorf("invalid slice subnet %s: %w", sliceSubnet, err)
	}

	// Build set of allocated subnets
	allocatedSubnets := make(map[string]bool)
	for _, allocation := range allocations {
		if allocation.Status == v1alpha1.AllocationStatusAllocated {
			allocatedSubnets[allocation.SubnetCIDR] = true
		}
	}

	// Calculate cluster subnet size (typically /24 for /16 slice subnet)
	sliceOnes, _ := sliceNet.Mask.Size()
	clusterSubnetBits := sliceOnes + 8 // Add 8 bits for cluster subnets (e.g., /16 -> /24)
	if clusterSubnetBits > 30 {
		clusterSubnetBits = 30 // Maximum /30 for point-to-point links
	}

	// Generate possible subnets and find first available
	baseIP := sliceNet.IP
	subnetSize := 1 << (32 - clusterSubnetBits)        // Number of IPs in each cluster subnet
	maxSubnets := 1 << (clusterSubnetBits - sliceOnes) // Maximum number of cluster subnets

	for i := 0; i < maxSubnets; i++ {
		// Calculate subnet IP
		subnetIP := make(net.IP, 4)
		copy(subnetIP, baseIP)

		// Add offset for this subnet
		offset := i * subnetSize
		for j := 3; j >= 0 && offset > 0; j-- {
			subnetIP[j] += byte(offset & 0xFF)
			offset >>= 8
		}

		subnetCIDR := fmt.Sprintf("%s/%d", subnetIP.String(), clusterSubnetBits)

		// Check if this subnet is available
		if !allocatedSubnets[subnetCIDR] {
			return subnetCIDR, nil
		}
	}

	return "", fmt.Errorf("no available subnets in slice %s", sliceSubnet)
}

// updateIPAMStatus updates the status of an IPAM allocation
func (d *DynamicIPAMService) updateIPAMStatus(ipamAllocation *v1alpha1.IPAMAllocation) {
	allocatedCount := 0
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.Status == v1alpha1.AllocationStatusAllocated {
			allocatedCount++
		}
	}

	// Calculate available subnets based on slice subnet
	_, sliceNet, err := net.ParseCIDR(ipamAllocation.Spec.SliceSubnet)
	availableCount := 0
	if err == nil {
		sliceOnes, _ := sliceNet.Mask.Size()
		clusterSubnetBits := sliceOnes + 8
		if clusterSubnetBits <= 30 {
			availableCount = (1 << (clusterSubnetBits - sliceOnes)) - allocatedCount
		}
	}

	ipamAllocation.Status = v1alpha1.IPAMAllocationStatus{
		AllocatedSubnets: allocatedCount,
		AvailableSubnets: availableCount,
		LastUpdated:      metav1.Now(),
	}
}

// getIPAMAllocationName generates the name for an IPAM allocation resource
func (d *DynamicIPAMService) getIPAMAllocationName(sliceName string) string {
	return fmt.Sprintf("ipam-%s", sliceName)
}
