/*
Copyright (c) 2026 Red Hat Inc.

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
	"math"
	"net"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

// PrivatePublicIPPoolsServerBuilder contains the data and logic needed to create a new private public IP pools server.
type PrivatePublicIPPoolsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.PublicIPPoolsServer = (*PrivatePublicIPPoolsServer)(nil)

// PrivatePublicIPPoolsServer is the private server for public IP pools.
type PrivatePublicIPPoolsServer struct {
	privatev1.UnimplementedPublicIPPoolsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.PublicIPPool]
}

// NewPrivatePublicIPPoolsServer creates a builder that can then be used to configure and create a new private public
// IP pools server.
func NewPrivatePublicIPPoolsServer() *PrivatePublicIPPoolsServerBuilder {
	return &PrivatePublicIPPoolsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *PrivatePublicIPPoolsServerBuilder) SetLogger(value *slog.Logger) *PrivatePublicIPPoolsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier used to publish change events.
func (b *PrivatePublicIPPoolsServerBuilder) SetNotifier(value events.Notifier) *PrivatePublicIPPoolsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic used to determine the creator of objects.
func (b *PrivatePublicIPPoolsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivatePublicIPPoolsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic used to determine the tenants of objects.
func (b *PrivatePublicIPPoolsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivatePublicIPPoolsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register metrics for the underlying database access
// objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivatePublicIPPoolsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivatePublicIPPoolsServerBuilder {
	b.metricsRegisterer = value
	return b
}

// Build uses the data stored in the builder to create a new private public IP pools server.
func (b *PrivatePublicIPPoolsServerBuilder) Build() (result *PrivatePublicIPPoolsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	generic, err := NewGenericServer[*privatev1.PublicIPPool]().
		SetLogger(b.logger).
		SetService(privatev1.PublicIPPools_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		AddAllowedTenants(auth.SharedTenant).
		Build()
	if err != nil {
		return
	}

	result = &PrivatePublicIPPoolsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivatePublicIPPoolsServer) List(ctx context.Context,
	request *privatev1.PublicIPPoolsListRequest) (response *privatev1.PublicIPPoolsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Get(ctx context.Context,
	request *privatev1.PublicIPPoolsGetRequest) (response *privatev1.PublicIPPoolsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Create(ctx context.Context,
	request *privatev1.PublicIPPoolsCreateRequest) (response *privatev1.PublicIPPoolsCreateResponse, err error) {
	pool := request.GetObject()

	err = s.validateCreate(ctx, pool)
	if err != nil {
		return
	}

	// At creation time nothing is allocated, so available == total.
	total := calculatePoolCapacity(pool.GetSpec().GetCidrs(), pool.GetSpec().GetIpFamily())
	pool.SetStatus(privatev1.PublicIPPoolStatus_builder{
		Total:     total,
		Available: total,
	}.Build())

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Update(ctx context.Context,
	request *privatev1.PublicIPPoolsUpdateRequest) (response *privatev1.PublicIPPoolsUpdateResponse, err error) {
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.PublicIPPoolsGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.PublicIPPoolsGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	err = validateUpdate(request.GetObject(), getResponse.GetObject())
	if err != nil {
		return
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

// validateCreate validates a PublicIPPool creation request.
func (s *PrivatePublicIPPoolsServer) validateCreate(ctx context.Context,
	pool *privatev1.PublicIPPool) error {

	if pool == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP pool is mandatory")
	}

	spec := pool.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP pool spec is mandatory")
	}

	if spec.GetIpFamily() == privatev1.IPFamily_IP_FAMILY_UNSPECIFIED {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.ip_family' is required")
	}

	cidrs := spec.GetCidrs()
	if len(cidrs) == 0 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.cidrs' is required")
	}

	for i, cidr := range cidrs {
		if err := validatePoolCIDRFormat(cidr, spec.GetIpFamily(), i); err != nil {
			return err
		}
	}

	if err := validateNoCIDRSelfOverlap(cidrs); err != nil {
		return err
	}

	return s.validateNoPoolCIDROverlap(ctx, pool)
}

// validateUpdate validates a PublicIPPool update request. Only immutability of spec fields is checked
func validateUpdate(newPool *privatev1.PublicIPPool, existing *privatev1.PublicIPPool) error {
	if newPool == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP pool is mandatory")
	}

	spec := newPool.GetSpec()
	if spec == nil {
		return nil
	}

	if spec.GetIpFamily() != privatev1.IPFamily_IP_FAMILY_UNSPECIFIED &&
		spec.GetIpFamily() != existing.GetSpec().GetIpFamily() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.ip_family' is immutable and cannot be changed from '%s' to '%s'",
			existing.GetSpec().GetIpFamily().String(), spec.GetIpFamily().String())
	}

	if newCIDRs := spec.GetCidrs(); len(newCIDRs) > 0 && !cidrSlicesEqual(newCIDRs, existing.GetSpec().GetCidrs()) {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.cidrs' is immutable and cannot be changed after creation")
	}

	return nil
}

// validatePoolCIDRFormat validates CIDR string is parseable and belongs to the specified IP family
func validatePoolCIDRFormat(cidrStr string, ipFamily privatev1.IPFamily, idx int) error {
	_, network, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"invalid CIDR format in field 'spec.cidrs[%d]': '%s': %v", idx, cidrStr, err)
	}

	isIPv4 := network.IP.To4() != nil
	switch ipFamily {
	case privatev1.IPFamily_IP_FAMILY_IPV4:
		if !isIPv4 {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.cidrs[%d]' contains IPv6 address but pool ip_family is IPv4: %s", idx, cidrStr)
		}
	case privatev1.IPFamily_IP_FAMILY_IPV6:
		if isIPv4 {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.cidrs[%d]' contains IPv4 address but pool ip_family is IPv6: %s", idx, cidrStr)
		}
	}

	return nil
}

// validateNoCIDRSelfOverlap checks that no two CIDRs within the same pool overlap each other.
// This is called after format validation, so all CIDRs are known to be parseable.
func validateNoCIDRSelfOverlap(cidrs []string) error {
	for i := 0; i < len(cidrs); i++ {
		for j := i + 1; j < len(cidrs); j++ {
			overlap, err := cidrsOverlap(cidrs[i], cidrs[j])
			if err != nil {
				return grpcstatus.Errorf(grpccodes.Internal, "failed to check intra-pool CIDR overlap")
			}
			if overlap {
				return grpcstatus.Errorf(grpccodes.InvalidArgument,
					"field 'spec.cidrs[%d]' (%s) overlaps with 'spec.cidrs[%d]' (%s) within the same pool",
					i, cidrs[i], j, cidrs[j])
			}
		}
	}
	return nil
}

// validateNoPoolCIDROverlap checks for CIDR overlap with CIDRs from any existing pool.
func (s *PrivatePublicIPPoolsServer) validateNoPoolCIDROverlap(ctx context.Context,
	pool *privatev1.PublicIPPool) error {

	newCIDRs := pool.GetSpec().GetCidrs()

	var offset int32
	for {
		listRequest := &privatev1.PublicIPPoolsListRequest{}
		listRequest.SetOffset(offset)
		var listResponse *privatev1.PublicIPPoolsListResponse
		if err := s.generic.List(ctx, listRequest, &listResponse); err != nil {
			s.logger.ErrorContext(ctx, "Failed to list pools for overlap check", slog.Any("error", err))
			return grpcstatus.Errorf(grpccodes.Internal, "failed to validate CIDR overlap")
		}

		for _, existing := range listResponse.GetItems() {
			for _, existingCIDR := range existing.GetSpec().GetCidrs() {
				for _, newCIDR := range newCIDRs {
					overlap, err := cidrsOverlap(newCIDR, existingCIDR)
					if err != nil {
						s.logger.ErrorContext(ctx, "Failed to check CIDR overlap", slog.Any("error", err))
						return grpcstatus.Errorf(grpccodes.Internal, "failed to validate CIDR overlap")
					}
					if overlap {
						return grpcstatus.Errorf(grpccodes.AlreadyExists,
							"CIDR '%s' overlaps with CIDR '%s' from existing pool '%s'",
							newCIDR, existingCIDR, existing.GetMetadata().GetName())
					}
				}
			}
		}

		if offset+listResponse.GetSize() >= listResponse.GetTotal() {
			break
		}
		offset += listResponse.GetSize()
	}

	return nil
}

// calculatePoolCapacity returns the total number of usable IP addresses across all CIDRs in the pool
func calculatePoolCapacity(cidrs []string, ipFamily privatev1.IPFamily) int64 {
	var total int64
	for _, cidr := range cidrs {
		cap := calculateCIDRCapacity(cidr, ipFamily)
		if total > math.MaxInt64-cap {
			return math.MaxInt64
		}
		total += cap
	}
	return total
}

// calculateCIDRCapacity returns the number of usable IP addresses for a single CIDR
func calculateCIDRCapacity(cidrStr string, ipFamily privatev1.IPFamily) int64 {
	_, network, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return 0
	}
	ones, bits := network.Mask.Size()
	if ones == 0 && bits == 0 {
		return 0
	}
	hostBits := bits - ones

	if ipFamily == privatev1.IPFamily_IP_FAMILY_IPV4 {
		total := int64(1) << hostBits
		if hostBits >= 2 {
			return total - 2 // subtract network and broadcast
		}
		return total // /31 = 2 (point-to-point), /32 = 1 (host route)
	}

	// IPv6: all addresses usable; cap at math.MaxInt64 for large prefix lengths.
	if hostBits >= 63 {
		return math.MaxInt64
	}
	return int64(1) << hostBits
}

// cidrSlicesEqual reports whether two CIDR slices contain the same set of CIDRs (order-independent).
func cidrSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := make([]string, len(a))
	sortedB := make([]string, len(b))
	copy(sortedA, a)
	copy(sortedB, b)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

func (s *PrivatePublicIPPoolsServer) Delete(ctx context.Context,
	request *privatev1.PublicIPPoolsDeleteRequest) (response *privatev1.PublicIPPoolsDeleteResponse, err error) {
	var getResponse *privatev1.PublicIPPoolsGetResponse
	err = s.generic.Get(ctx, privatev1.PublicIPPoolsGetRequest_builder{
		Id: request.GetId(),
	}.Build(), &getResponse)
	if err != nil {
		return
	}
	if allocated := getResponse.GetObject().GetStatus().GetAllocated(); allocated > 0 {
		err = grpcstatus.Errorf(
			grpccodes.FailedPrecondition,
			"cannot delete public IP pool '%s': %d public IP(s) are still allocated from it",
			request.GetId(), allocated,
		)
		return
	}
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Signal(ctx context.Context,
	request *privatev1.PublicIPPoolsSignalRequest) (response *privatev1.PublicIPPoolsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
