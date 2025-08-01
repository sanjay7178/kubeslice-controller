# Dynamic IPAM for KubeSlice Controller

This document describes the Dynamic IP Address Management (IPAM) system implemented for the KubeSlice controller to replace the static subnet allocation approach.

## Overview

The dynamic IPAM system provides efficient IP address management for slice overlay networks by:

- **On-demand allocation**: Allocates IP subnets only when clusters join a slice
- **Automatic reclamation**: Releases subnets when clusters leave a slice
- **Conflict resolution**: Prevents subnet conflicts across slices
- **State synchronization**: Uses Kubernetes CRDs for distributed state management

## Architecture

### Components

1. **IPAMAllocation CRD**: Tracks subnet allocations per slice
2. **DynamicIPAMService**: Core service implementing allocation/deallocation logic
3. **Enhanced SliceConfigService**: Integrates dynamic IPAM with slice configuration
4. **Updated Gateway Services**: Works with dynamically allocated subnets

### IPAMAllocation CRD

```yaml
apiVersion: controller.kubeslice.io/v1alpha1
kind: IPAMAllocation
metadata:
  name: ipam-my-slice
  namespace: kubeslice-project
spec:
  sliceName: my-slice
  sliceSubnet: "10.1.0.0/16"
  clusterAllocations:
  - clusterName: "cluster-1"
    subnetCIDR: "10.1.0.0/24"
    allocatedAt: "2024-01-01T00:00:00Z"
    status: "Allocated"
  - clusterName: "cluster-2"
    subnetCIDR: "10.1.1.0/24"
    allocatedAt: "2024-01-01T00:05:00Z"
    status: "Allocated"
status:
  allocatedSubnets: 2
  availableSubnets: 254
  lastUpdated: "2024-01-01T00:05:00Z"
```

## Usage

### Enabling Dynamic IPAM

Set the `sliceIpamType` field to `"Dynamic"` in your SliceConfig:

```yaml
apiVersion: controller.kubeslice.io/v1alpha1
kind: SliceConfig
metadata:
  name: my-slice
  namespace: kubeslice-project
spec:
  sliceSubnet: "10.1.0.0/16"
  sliceIpamType: "Dynamic"  # Enable dynamic IPAM
  clusters:
    - cluster-1
    - cluster-2
  # ... other configuration
```

### Backward Compatibility

If `sliceIpamType` is not set or is set to a value other than `"Dynamic"`, the system falls back to the legacy static IPAM mode for backward compatibility.

## Key Features

### 1. On-Demand Allocation

When a cluster joins a slice, the system automatically:
- Calculates the next available subnet from the slice CIDR
- Creates or updates the IPAMAllocation resource
- Returns the allocated subnet for use in network configuration

### 2. Automatic Reclamation

When a cluster leaves a slice:
- The allocation status is changed to "Released"
- The subnet becomes available for future allocation
- Network resources are cleaned up appropriately

### 3. Conflict Prevention

The system prevents subnet conflicts by:
- Tracking all allocations in the IPAMAllocation CRD
- Using sequential allocation to avoid overlaps
- Validating subnet availability before allocation

### 4. State Synchronization

State is maintained using Kubernetes CRDs, providing:
- Persistent storage of allocation state
- Distributed access across cluster components
- Kubernetes-native conflict resolution

## API Methods

### DynamicIPAMService Interface

```go
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
```

### Example Usage

```go
// Initialize the service
ipamService := service.NewDynamicIPAMService()

// Allocate subnet for a cluster
subnetCIDR, err := ipamService.AllocateSubnetForCluster(
    ctx, "my-slice", "10.1.0.0/16", "cluster-1", "kubeslice-project")
if err != nil {
    // Handle error
}
// subnetCIDR will be something like "10.1.0.0/24"

// Reconcile allocations when cluster list changes
clusters := []string{"cluster-1", "cluster-2", "cluster-3"}
err = ipamService.ReconcileIPAMAllocation(
    ctx, "my-slice", "10.1.0.0/16", "kubeslice-project", clusters)

// Deallocate when cluster leaves
err = ipamService.DeallocateSubnetForCluster(
    ctx, "my-slice", "cluster-1", "kubeslice-project")
```

## Subnet Allocation Algorithm

The dynamic IPAM system uses a sequential allocation strategy:

1. **Parse slice subnet**: Extract network and mask information
2. **Calculate cluster subnet size**: Typically /24 subnets from /16 slice subnet
3. **Find next available**: Iterate through possible subnets to find unallocated one
4. **Validate availability**: Check against existing allocations
5. **Allocate and record**: Create allocation record with timestamp

### Example Allocation Sequence

For slice subnet `10.1.0.0/16`:
- Cluster-1: `10.1.0.0/24`
- Cluster-2: `10.1.1.0/24`
- Cluster-3: `10.1.2.0/24`
- ...and so on

## Integration Points

### SliceConfigService

The `SliceConfigService` has been enhanced to:
- Detect when dynamic IPAM is enabled
- Call `ReconcileIPAMAllocation()` during slice reconciliation
- Pass IPAM service to worker slice config creation

### WorkerSliceConfigService

New method `CreateMinimalWorkerSliceConfigWithDynamicIPAM()`:
- Allocates subnets for each cluster dynamically
- Creates worker slice configs with allocated subnets
- Updates existing configs when subnets change

### WorkerSliceGatewayService

Enhanced with dynamic IPAM support:
- `CreateMinimumWorkerSliceGatewaysWithDynamicIPAM()`
- `BuildNetworkAddressesWithDynamicIPAM()`
- Retrieves allocated subnets for gateway configuration

## Benefits

### Efficient IP Space Utilization
- No waste from pre-allocated unused subnets
- Optimal utilization based on actual cluster participation

### Scalability
- Supports dynamic cluster addition/removal
- No upfront planning for maximum cluster count

### Operational Simplicity
- Automatic subnet management
- No manual IP address coordination required

### Resource Efficiency
- Reduces IP address space requirements
- Enables denser slice deployments

## Migration Path

Existing deployments can gradually migrate to dynamic IPAM:

1. **Immediate compatibility**: Existing slices continue to work with static IPAM
2. **Opt-in migration**: Set `sliceIpamType: "Dynamic"` on new or updated slices
3. **Gradual rollout**: Migrate slices one at a time as needed

## Security Considerations

- RBAC permissions include IPAMAllocation resources
- Allocation state is stored in Kubernetes etcd with appropriate access controls
- No sensitive data stored in allocation records

## Monitoring and Observability

The system provides:
- Allocation status in IPAMAllocation CRD status
- Event recording for allocation/deallocation operations
- Metrics integration with existing KubeSlice monitoring

## Future Enhancements

Potential improvements for future versions:
- Support for custom subnet sizing
- Cross-slice conflict detection
- Advanced allocation strategies (e.g., geographic-based)
- Integration with external IPAM systems
- Automatic subnet defragmentation