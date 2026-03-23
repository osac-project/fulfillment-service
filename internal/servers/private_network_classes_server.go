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

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type PrivateNetworkClassesServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
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

func (b *PrivateNetworkClassesServerBuilder) SetNotifier(value *database.Notifier) *PrivateNetworkClassesServerBuilder {
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

	err = s.generic.Create(ctx, request, &response)
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
	}

	return nil
}

// cloneNetworkClass creates a deep copy of a NetworkClass.
func cloneNetworkClass(nc *privatev1.NetworkClass) *privatev1.NetworkClass {
	return proto.Clone(nc).(*privatev1.NetworkClass)
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
		default:
			// For unknown paths, fall through - the generic handler will
			// reject invalid paths if needed.
		}
	}
}
