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
	"net/netip"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
)

// findDefaultNetworkClass returns the current default NetworkClass using the provided DAO, or nil if none is set.
// If multiple defaults exist (invariant violation), it returns the newest and logs a warning.
func findDefaultNetworkClass(ctx context.Context, logger *slog.Logger, ncDao *dao.GenericDAO[*privatev1.NetworkClass]) (*privatev1.NetworkClass, error) {
	listResponse, err := ncDao.List().
		SetFilter("this.is_default == true").
		Do(ctx)
	if err != nil {
		return nil, err
	}
	// Exclude soft-deleted records — the DAO does not filter these automatically, and a
	// recently-deleted default NC must not be returned as the active default.
	var items []*privatev1.NetworkClass
	for _, nc := range listResponse.GetItems() {
		if !nc.GetMetadata().HasDeletionTimestamp() {
			items = append(items, nc)
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	// Sort newest-first so that the invariant-violation fallback is deterministic.
	sort.Slice(items, func(i, j int) bool {
		ti := items[i].GetMetadata().GetCreationTimestamp().AsTime()
		tj := items[j].GetMetadata().GetCreationTimestamp().AsTime()
		return ti.After(tj)
	})
	if len(items) > 1 {
		logger.WarnContext(ctx, "multiple default NetworkClasses found, using newest",
			slog.Int("count", len(items)),
		)
	}
	return items[0], nil
}

type PrivateNetworkClassesServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.NetworkClassesServer = (*PrivateNetworkClassesServer)(nil)

type PrivateNetworkClassesServer struct {
	privatev1.UnimplementedNetworkClassesServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.NetworkClass]
}

func NewPrivateNetworkClassesServer() *PrivateNetworkClassesServerBuilder {
	return &PrivateNetworkClassesServerBuilder{}
}

func (b *PrivateNetworkClassesServerBuilder) SetLogger(value *slog.Logger) *PrivateNetworkClassesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateNetworkClassesServerBuilder) SetNotifier(value events.Notifier) *PrivateNetworkClassesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateNetworkClassesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateNetworkClassesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateNetworkClassesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateNetworkClassesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateNetworkClassesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateNetworkClassesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateNetworkClassesServerBuilder) Build() (result *PrivateNetworkClassesServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.NetworkClass]().
		SetLogger(b.logger).
		SetService(privatev1.NetworkClasses_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateNetworkClassesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateNetworkClassesServer) List(ctx context.Context,
	request *privatev1.NetworkClassesListRequest) (response *privatev1.NetworkClassesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateNetworkClassesServer) Get(ctx context.Context,
	request *privatev1.NetworkClassesGetRequest) (response *privatev1.NetworkClassesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateNetworkClassesServer) Create(ctx context.Context,
	request *privatev1.NetworkClassesCreateRequest) (response *privatev1.NetworkClassesCreateResponse, err error) {
	// Validate before creating:
	err = s.validateNetworkClass(ctx, request.GetObject(), nil)
	if err != nil {
		return
	}

	// Set status to READY on creation since NetworkClass has no backend provisioning.
	nc := request.GetObject()
	if nc.Status == nil {
		nc.Status = &privatev1.NetworkClassStatus{}
	}
	nc.Status.SetState(privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

	// Clear any caller-provided ID so the DAO always generates a UUID.
	nc.SetId("")

	// Default-swap: if this NC is being created as the default, unset all existing defaults.
	// Both the old-default unset(s) and the new-NC persist share this request's database transaction via ctx.
	if nc.GetIsDefault() {
		err = s.clearExistingDefaults(ctx, "")
		if err != nil {
			return nil, err
		}
	}

	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		// When creating a default NC, a concurrent default-swap triggers the unique partial
		// index (network_classes_single_default). The error path is:
		//   DAO catches UniqueViolation → wraps as ErrAlreadyExists (discards pgconn.PgError)
		//   → GenericServer maps ErrAlreadyExists → gRPC AlreadyExists status error.
		// Since we generate fresh UUIDs (line 178 clears caller-provided ID), a primary key
		// collision is impossible — so a gRPC AlreadyExists here means a concurrent
		// default-swap conflict from the unique partial index.
		if nc.GetIsDefault() {
			if st, ok := grpcstatus.FromError(err); ok && st.Code() == grpccodes.AlreadyExists {
				return nil, grpcstatus.Errorf(grpccodes.FailedPrecondition,
					"concurrent default NetworkClass change detected, please retry")
			}
		}
	}
	return
}

func (s *PrivateNetworkClassesServer) Update(ctx context.Context,
	request *privatev1.NetworkClassesUpdateRequest) (response *privatev1.NetworkClassesUpdateResponse, err error) {
	// Get existing object for immutability validation:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.NetworkClassesGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.NetworkClassesGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingNC := getResponse.GetObject()

	// Merge the update into the existing object so that required-field
	// validation works correctly for partial updates (field mask).
	merged := cloneNetworkClass(existingNC)
	applyNetworkClassUpdate(merged, request.GetObject(), request.GetUpdateMask())

	// Validate the merged result against the original for immutability checks:
	err = s.validateNetworkClass(ctx, merged, existingNC)
	if err != nil {
		return
	}

	// Default-swap: if the update sets is_default=true AND the field is actually being applied
	// (nil mask = full update, or "is_default" is in the mask), unset all other existing defaults.
	// Both the old-default unset(s) and the NC persist share this request's database transaction via ctx.
	if request.GetObject().HasIsDefault() && request.GetObject().GetIsDefault() {
		shouldSwap := true
		if mask := request.GetUpdateMask(); mask != nil && len(mask.GetPaths()) > 0 {
			shouldSwap = false
			for _, path := range mask.GetPaths() {
				if path == "is_default" {
					shouldSwap = true
					break
				}
			}
		}
		if shouldSwap {
			err = s.clearExistingDefaults(ctx, id)
			if err != nil {
				return nil, err
			}
		}
	}

	// NOTE: On the Update path, a concurrent default-swap UniqueViolation is normalized
	// to gRPC Internal by GenericServer (the DAO Update does not catch UniqueViolation,
	// and GenericServer wraps unknown errors as Internal). We cannot distinguish the
	// constraint violation from other Internal errors at this layer. The error is
	// normalized (no raw DB details leak), but the message is opaque. Fixing this
	// requires GenericServer to preserve pgconn errors in the chain.
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateNetworkClassesServer) Delete(ctx context.Context,
	request *privatev1.NetworkClassesDeleteRequest) (response *privatev1.NetworkClassesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateNetworkClassesServer) Signal(ctx context.Context,
	request *privatev1.NetworkClassesSignalRequest) (response *privatev1.NetworkClassesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// clearExistingDefaults fetches all NetworkClasses with is_default == true, except the one with
// the given excludeID, and clears the is_default flag on each. Used during default-swap to ensure
// only one default exists at a time even if the invariant was previously violated.
//
// Concurrent default-swap requests are safe: the unique partial index network_classes_single_default
// (migration 28) prevents two concurrent transactions from both committing is_default=true.
// The losing transaction receives a unique constraint violation from the database.
func (s *PrivateNetworkClassesServer) clearExistingDefaults(ctx context.Context, excludeID string) error {
	listResponse, err := s.generic.dao.List().
		SetFilter("this.is_default == true").
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to list default NetworkClasses",
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(grpccodes.Internal, "failed to clear existing default NetworkClasses")
	}
	for _, nc := range listResponse.GetItems() {
		if nc.GetId() == excludeID {
			continue
		}
		// Skip soft-deleted NCs: calling dao.Update on a soft-deleted NC with no finalizers
		// triggers archiving (generic_dao_update.go), which is an unintended side effect.
		if nc.GetMetadata().HasDeletionTimestamp() {
			continue
		}
		s.logger.InfoContext(ctx, "unsetting previous default NetworkClass",
			"old_default_id", nc.GetId(),
		)
		nc.ClearIsDefault()
		_, err = s.generic.dao.Update().SetObject(nc).Do(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to clear default on NetworkClass",
				slog.String("network_class_id", nc.GetId()),
				slog.Any("error", err),
			)
			return grpcstatus.Errorf(grpccodes.Internal, "failed to clear existing default NetworkClasses")
		}
	}
	return nil
}

// validateNetworkClass validates the NetworkClass object.
func (s *PrivateNetworkClassesServer) validateNetworkClass(ctx context.Context,
	newNC *privatev1.NetworkClass, existingNC *privatev1.NetworkClass) error {

	if newNC == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "network class is mandatory")
	}

	// NC-VAL-01: implementation_strategy is required
	if newNC.GetImplementationStrategy() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'implementation_strategy' is required")
	}

	// NC-VAL-02: title is required
	if newNC.GetTitle() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'title' is required")
	}

	// NC-VAL-03: Validate capabilities consistency
	caps := newNC.GetCapabilities()
	if caps != nil {
		// If dual-stack is supported, both IPv4 and IPv6 must be supported
		if caps.GetSupportsDualStack() && (!caps.GetSupportsIpv4() || !caps.GetSupportsIpv6()) {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"if 'capabilities.supports_dual_stack' is true, both 'supports_ipv4' and 'supports_ipv6' must be true")
		}
	}

	// NC-VAL-04: Check immutable fields (only on Update)
	if existingNC != nil {
		// implementation_strategy is immutable
		if newNC.GetImplementationStrategy() != existingNC.GetImplementationStrategy() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'implementation_strategy' is immutable and cannot be changed from '%s' to '%s'",
				existingNC.GetImplementationStrategy(), newNC.GetImplementationStrategy())
		}

		// NC-VAL-06: fabric_manager is immutable
		if newNC.GetFabricManager() != existingNC.GetFabricManager() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'fabric_manager' is immutable and cannot be changed from '%s' to '%s'",
				existingNC.GetFabricManager(), newNC.GetFabricManager())
		}

		// NC-VAL-07: k8s_manager is immutable once set (but can be set for the first time)
		if existingNC.HasK8SManager() && newNC.GetK8SManager() != existingNC.GetK8SManager() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'k8s_manager' is immutable and cannot be changed from '%s' to '%s'",
				existingNC.GetK8SManager(), newNC.GetK8SManager())
		}
	}

	// NC-VAL-05: fabric_manager is required
	if newNC.GetFabricManager() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'fabric_manager' is required")
	}

	// NC-VAL-08: Validate defaults if present
	if defaults := newNC.GetDefaults(); defaults != nil {
		if err := validateNetworkDefaults(defaults); err != nil {
			return err
		}
	}

	return nil
}

// cloneNetworkClass creates a deep copy of a NetworkClass.
func cloneNetworkClass(nc *privatev1.NetworkClass) *privatev1.NetworkClass {
	return proto.Clone(nc).(*privatev1.NetworkClass)
}

// validateNetworkDefaults validates the NetworkDefaults configuration.
func validateNetworkDefaults(defaults *privatev1.NetworkDefaults) error {
	vnCIDR := defaults.GetVirtualNetworkCidr()
	subnetCIDR := defaults.GetSubnetCidr()

	var vnPrefix netip.Prefix
	if vnCIDR != "" {
		parsed, err := parseAndValidateCIDR(vnCIDR, cidrIPv4)
		if err != nil {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"defaults.virtual_network_cidr: %v", err)
		}
		vnPrefix, err = netip.ParsePrefix(parsed)
		if err != nil {
			return grpcstatus.Errorf(grpccodes.Internal,
				"defaults.virtual_network_cidr: failed to re-parse validated CIDR '%s': %v", parsed, err)
		}
	}

	if subnetCIDR != "" {
		if vnCIDR == "" {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"defaults.subnet_cidr requires defaults.virtual_network_cidr to be set")
		}

		_, err := parseAndValidateCIDR(subnetCIDR, cidrIPv4)
		if err != nil {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"defaults.subnet_cidr: %v", err)
		}

		subnetPrefix, err := netip.ParsePrefix(subnetCIDR)
		if err != nil {
			return grpcstatus.Errorf(grpccodes.Internal,
				"defaults.subnet_cidr: failed to re-parse validated CIDR '%s': %v", subnetCIDR, err)
		}
		subnetWithinVN := vnPrefix.Contains(subnetPrefix.Addr()) && subnetPrefix.Bits() >= vnPrefix.Bits()
		if !subnetWithinVN {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"defaults.subnet_cidr '%s' is not within defaults.virtual_network_cidr '%s'",
				subnetCIDR, vnCIDR)
		}
	}

	for i, rule := range defaults.GetIngressRules() {
		if err := validateSecurityRule(rule, "defaults.ingress", i); err != nil {
			return err
		}
	}
	for i, rule := range defaults.GetEgressRules() {
		if err := validateSecurityRule(rule, "defaults.egress", i); err != nil {
			return err
		}
	}

	return nil
}

// applyNetworkClassUpdate applies the update fields onto the base object,
// respecting the field mask. If no mask is provided, all fields from the
// update are applied.
func applyNetworkClassUpdate(base, update *privatev1.NetworkClass, mask *fieldmaskpb.FieldMask) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		proto.Merge(base, update)
		return
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "status.state":
			if base.Status == nil {
				base.Status = &privatev1.NetworkClassStatus{}
			}
			base.Status.SetState(update.GetStatus().GetState())
		case "status.message":
			if base.Status == nil {
				base.Status = &privatev1.NetworkClassStatus{}
			}
			base.Status.SetMessage(update.GetStatus().GetMessage())
		case "title":
			base.SetTitle(update.GetTitle())
		case "description":
			base.SetDescription(update.GetDescription())
		case "implementation_strategy":
			base.SetImplementationStrategy(update.GetImplementationStrategy())
		case "capabilities":
			base.SetCapabilities(update.GetCapabilities())
		case "is_default":
			base.SetIsDefault(update.GetIsDefault())
		case "fabric_manager":
			base.SetFabricManager(update.GetFabricManager())
		case "k8s_manager":
			if update.HasK8SManager() {
				base.SetK8SManager(update.GetK8SManager())
			} else {
				base.ClearK8SManager()
			}
		case "defaults":
			base.SetDefaults(update.GetDefaults())
		default:
			// For unknown paths, fall through - the generic handler will
			// reject invalid paths if needed.
		}
	}
}
