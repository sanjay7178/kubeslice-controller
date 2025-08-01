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
	"testing"

	"github.com/kubeslice/kubeslice-controller/apis/controller/v1alpha1"
	"github.com/kubeslice/kubeslice-controller/util"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type DynamicIPAMServiceTestSuite struct {
	suite.Suite
	service IDynamicIPAMService
	client  *fake.ClientBuilder
	ctx     context.Context
}

func (suite *DynamicIPAMServiceTestSuite) SetupTest() {
	scheme := runtime.NewScheme()
	v1alpha1.AddToScheme(scheme)
	
	suite.client = fake.NewClientBuilder().WithScheme(scheme)
	util.SetClient(suite.client.Build())
	
	suite.service = NewDynamicIPAMService()
	suite.ctx = context.Background()
}

func (suite *DynamicIPAMServiceTestSuite) TestAllocateSubnetForCluster() {
	// Test allocating subnet for a cluster
	sliceName := "test-slice"
	sliceSubnet := "10.1.0.0/16"
	clusterName := "cluster-1"
	namespace := "kubeslice-test"

	// First allocation should succeed
	subnetCIDR, err := suite.service.AllocateSubnetForCluster(suite.ctx, sliceName, sliceSubnet, clusterName, namespace)
	suite.NoError(err)
	suite.NotEmpty(subnetCIDR)

	// Verify IPAM allocation resource was created
	ipamAllocation := &v1alpha1.IPAMAllocation{}
	err = util.GetResource(suite.ctx, types.NamespacedName{
		Name:      "ipam-" + sliceName,
		Namespace: namespace,
	}, ipamAllocation)
	suite.NoError(err)
	suite.Equal(sliceName, ipamAllocation.Spec.SliceName)
	suite.Equal(sliceSubnet, ipamAllocation.Spec.SliceSubnet)
	suite.Len(ipamAllocation.Spec.ClusterAllocations, 1)
	suite.Equal(clusterName, ipamAllocation.Spec.ClusterAllocations[0].ClusterName)
	suite.Equal(v1alpha1.AllocationStatusAllocated, ipamAllocation.Spec.ClusterAllocations[0].Status)

	// Second allocation for same cluster should return same subnet
	subnetCIDR2, err := suite.service.AllocateSubnetForCluster(suite.ctx, sliceName, sliceSubnet, clusterName, namespace)
	suite.NoError(err)
	suite.Equal(subnetCIDR, subnetCIDR2)
}

func (suite *DynamicIPAMServiceTestSuite) TestDeallocateSubnetForCluster() {
	sliceName := "test-slice"
	sliceSubnet := "10.1.0.0/16"
	clusterName := "cluster-1"
	namespace := "kubeslice-test"

	// First allocate a subnet
	_, err := suite.service.AllocateSubnetForCluster(suite.ctx, sliceName, sliceSubnet, clusterName, namespace)
	suite.NoError(err)

	// Then deallocate it
	err = suite.service.DeallocateSubnetForCluster(suite.ctx, sliceName, clusterName, namespace)
	suite.NoError(err)

	// Verify allocation status is changed to released
	ipamAllocation := &v1alpha1.IPAMAllocation{}
	err = util.GetResource(suite.ctx, types.NamespacedName{
		Name:      "ipam-" + sliceName,
		Namespace: namespace,
	}, ipamAllocation)
	suite.NoError(err)
	suite.Len(ipamAllocation.Spec.ClusterAllocations, 1)
	suite.Equal(v1alpha1.AllocationStatusReleased, ipamAllocation.Spec.ClusterAllocations[0].Status)
}

func (suite *DynamicIPAMServiceTestSuite) TestReconcileIPAMAllocation() {
	sliceName := "test-slice"
	sliceSubnet := "10.1.0.0/16"
	namespace := "kubeslice-test"
	clusters := []string{"cluster-1", "cluster-2", "cluster-3"}

	// Test reconciliation with new clusters
	err := suite.service.ReconcileIPAMAllocation(suite.ctx, sliceName, sliceSubnet, namespace, clusters)
	suite.NoError(err)

	// Verify allocations were created for all clusters
	ipamAllocation := &v1alpha1.IPAMAllocation{}
	err = util.GetResource(suite.ctx, types.NamespacedName{
		Name:      "ipam-" + sliceName,
		Namespace: namespace,
	}, ipamAllocation)
	suite.NoError(err)
	suite.Len(ipamAllocation.Spec.ClusterAllocations, 3)
	
	// Verify all clusters have allocated status
	allocatedClusters := make(map[string]bool)
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		suite.Equal(v1alpha1.AllocationStatusAllocated, allocation.Status)
		allocatedClusters[allocation.ClusterName] = true
	}
	
	for _, cluster := range clusters {
		suite.True(allocatedClusters[cluster], "Cluster %s should be allocated", cluster)
	}

	// Test reconciliation with removed cluster
	updatedClusters := []string{"cluster-1", "cluster-2"}
	err = suite.service.ReconcileIPAMAllocation(suite.ctx, sliceName, sliceSubnet, namespace, updatedClusters)
	suite.NoError(err)

	// Verify cluster-3 allocation is marked as released
	err = util.GetResource(suite.ctx, types.NamespacedName{
		Name:      "ipam-" + sliceName,
		Namespace: namespace,
	}, ipamAllocation)
	suite.NoError(err)
	
	allocatedCount := 0
	releasedCount := 0
	for _, allocation := range ipamAllocation.Spec.ClusterAllocations {
		if allocation.Status == v1alpha1.AllocationStatusAllocated {
			allocatedCount++
		} else if allocation.Status == v1alpha1.AllocationStatusReleased {
			releasedCount++
			suite.Equal("cluster-3", allocation.ClusterName)
		}
	}
	suite.Equal(2, allocatedCount)
	suite.Equal(1, releasedCount)
}

func TestDynamicIPAMServiceTestSuite(t *testing.T) {
	suite.Run(t, new(DynamicIPAMServiceTestSuite))
}