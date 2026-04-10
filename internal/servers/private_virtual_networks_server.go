/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

type PrivateVirtualNetworksServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.VirtualNetworksServer = (*PrivateVirtualNetworksServer)(nil)

type PrivateVirtualNetworksServer struct {
	privatev1.UnimplementedVirtualNetworksServer

	logger          *slog.Logger
	generic         *GenericServer[*privatev1.VirtualNetwork]
	networkClassDao *dao.GenericDAO[*privatev1.NetworkClass]
}

func NewPrivateVirtualNetworksServer() *PrivateVirtualNetworksServerBuilder {
	return &PrivateVirtualNetworksServerBuilder{}
}

func (b *PrivateVirtualNetworksServerBuilder) SetLogger(value *slog.Logger) *PrivateVirtualNetworksServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateVirtualNetworksServerBuilder) SetNotifier(value *database.Notifier) *PrivateVirtualNetworksServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateVirtualNetworksServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateVirtualNetworksServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateVirtualNetworksServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateVirtualNetworksServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateVirtualNetworksServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateVirtualNetworksServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateVirtualNetworksServerBuilder) Build() (result *PrivateVirtualNetworksServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the NetworkClass DAO:
	networkClassDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.VirtualNetwork]().
		SetLogger(b.logger).
		SetService(privatev1.VirtualNetworks_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateVirtualNetworksServer{
		logger:          b.logger,
		generic:         generic,
		networkClassDao: networkClassDao,
	}
	return
}

func (s *PrivateVirtualNetworksServer) List(ctx context.Context,
	request *privatev1.VirtualNetworksListRequest) (response *privatev1.VirtualNetworksListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateVirtualNetworksServer) Get(ctx context.Context,
	request *privatev1.VirtualNetworksGetRequest) (response *privatev1.VirtualNetworksGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateVirtualNetworksServer) Create(ctx context.Context,
	request *privatev1.VirtualNetworksCreateRequest) (response *privatev1.VirtualNetworksCreateResponse, err error) {
	// Validate before creating:
	implementationStrategy, err := s.validateVirtualNetwork(ctx, request.GetObject(), nil)
	if err != nil {
		return
	}

	// Set the implementation_strategy from the NetworkClass
	// This is a system-managed field derived from the selected network_class
	if request.GetObject().GetSpec() != nil && implementationStrategy != "" {
		request.GetObject().GetSpec().SetImplementationStrategy(implementationStrategy)
	}

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateVirtualNetworksServer) Update(ctx context.Context,
	request *privatev1.VirtualNetworksUpdateRequest) (response *privatev1.VirtualNetworksUpdateResponse, err error) {
	// Get existing object for immutability validation:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.VirtualNetworksGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.VirtualNetworksGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingVN := getResponse.GetObject()

	// Validate with existing object context:
	_, err = s.validateVirtualNetwork(ctx, request.GetObject(), existingVN)
	if err != nil {
		return
	}

	// Preserve implementation_strategy from existing object (it's immutable)
	if request.GetObject().GetSpec() != nil && existingVN.GetSpec() != nil {
		request.GetObject().GetSpec().SetImplementationStrategy(existingVN.GetSpec().GetImplementationStrategy())
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateVirtualNetworksServer) Delete(ctx context.Context,
	request *privatev1.VirtualNetworksDeleteRequest) (response *privatev1.VirtualNetworksDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateVirtualNetworksServer) Signal(ctx context.Context,
	request *privatev1.VirtualNetworksSignalRequest) (response *privatev1.VirtualNetworksSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validateVirtualNetwork validates the VirtualNetwork object.
// Returns the implementation_strategy from the NetworkClass (only populated on Create).
func (s *PrivateVirtualNetworksServer) validateVirtualNetwork(ctx context.Context,
	newVN *privatev1.VirtualNetwork, existingVN *privatev1.VirtualNetwork) (implementationStrategy string, err error) {

	if newVN == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "virtual network is mandatory")
		return
	}

	spec := newVN.GetSpec()
	if spec == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "virtual network spec is mandatory")
		return
	}

	// VN-VAL-08: Region is required
	if spec.GetRegion() == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.region' is required")
		return
	}

	// VN-VAL-03: At least one CIDR must be provided
	if spec.GetIpv4Cidr() == "" && spec.GetIpv6Cidr() == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument,
			"at least one of 'spec.ipv4_cidr' or 'spec.ipv6_cidr' must be provided")
		return
	}

	// VN-VAL-01: Validate IPv4 CIDR format
	if spec.GetIpv4Cidr() != "" {
		if err = validateCIDR(spec.GetIpv4Cidr(), "IPv4"); err != nil {
			return
		}
	}

	// VN-VAL-02: Validate IPv6 CIDR format
	if spec.GetIpv6Cidr() != "" {
		if err = validateCIDR(spec.GetIpv6Cidr(), "IPv6"); err != nil {
			return
		}
	}

	// VN-VAL-09, VN-VAL-10: Check immutable fields (only on Update)
	if err = validateImmutableFields(newVN, existingVN); err != nil {
		return
	}

	// VN-VAL-04, VN-VAL-05, VN-VAL-06: Validate NetworkClass
	// Only validate on Create or if network_class changed (though it shouldn't on Update)
	if existingVN == nil || spec.GetNetworkClass() != existingVN.GetSpec().GetNetworkClass() {
		implementationStrategy, err = s.validateNetworkClassReference(ctx, spec)
		if err != nil {
			return
		}
	}

	return
}

// validateCIDR validates a CIDR string and checks if it matches the expected IP version.
func validateCIDR(cidrStr string, ipVersion string) error {
	_, network, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"invalid %s CIDR format '%s': %v", ipVersion, cidrStr, err)
	}

	// Validate IP version matches field name
	isIPv4 := network.IP.To4() != nil
	if ipVersion == "IPv4" && !isIPv4 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'ipv4_cidr' contains IPv6 address: %s", cidrStr)
	}
	if ipVersion == "IPv6" && isIPv4 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'ipv6_cidr' contains IPv4 address: %s", cidrStr)
	}

	return nil
}

// validateImmutableFields validates that immutable fields have not been changed.
func validateImmutableFields(newVN *privatev1.VirtualNetwork, existingVN *privatev1.VirtualNetwork) error {
	if existingVN == nil {
		return nil // Create operation, no immutability checks
	}

	newSpec := newVN.GetSpec()
	existingSpec := existingVN.GetSpec()

	// Check immutable region field
	if newSpec.GetRegion() != existingSpec.GetRegion() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.region' is immutable and cannot be changed from '%s' to '%s'",
			existingSpec.GetRegion(), newSpec.GetRegion())
	}

	// Check immutable network_class field
	if newSpec.GetNetworkClass() != existingSpec.GetNetworkClass() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.network_class' is immutable and cannot be changed from '%s' to '%s'",
			existingSpec.GetNetworkClass(), newSpec.GetNetworkClass())
	}

	return nil
}

// validateNetworkClassReference validates that the referenced NetworkClass exists and is in READY state.
// Returns the implementation_strategy from the NetworkClass for storage in VirtualNetwork spec.
func (s *PrivateVirtualNetworksServer) validateNetworkClassReference(ctx context.Context,
	spec *privatev1.VirtualNetworkSpec) (implementationStrategy string, err error) {

	networkClassID := spec.GetNetworkClass()
	if networkClassID == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.network_class' is required")
		return
	}

	// Look up NetworkClass by ID
	getResponse, err := s.networkClassDao.Get().
		SetId(networkClassID).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument,
				"network_class '%s' does not exist", networkClassID)
			return
		}
		s.logger.ErrorContext(ctx, "Failed to query NetworkClass",
			slog.String("network_class", networkClassID),
			slog.Any("error", err))
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to validate network_class")
		return
	}

	networkClass := getResponse.GetObject()

	// VN-VAL-05: Check NetworkClass is READY
	if networkClass.GetStatus().GetState() != privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY {
		err = grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"network_class '%s' is not in READY state (current state: %s)",
			networkClassID, networkClass.GetStatus().GetState().String())
		return
	}

	// VN-VAL-06: Validate capabilities match
	vnCaps := spec.GetCapabilities()
	ncCaps := networkClass.GetCapabilities()
	if vnCaps != nil && ncCaps != nil {
		if vnCaps.GetEnableIpv4() && !ncCaps.GetSupportsIpv4() {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument,
				"network_class '%s' does not support IPv4", networkClassID)
			return
		}
		if vnCaps.GetEnableIpv6() && !ncCaps.GetSupportsIpv6() {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument,
				"network_class '%s' does not support IPv6", networkClassID)
			return
		}
		if vnCaps.GetEnableDualStack() && !ncCaps.GetSupportsDualStack() {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument,
				"network_class '%s' does not support dual-stack", networkClassID)
			return
		}
	}

	// Return the implementation_strategy for storage in VirtualNetwork spec
	implementationStrategy = networkClass.GetImplementationStrategy()
	return
}
