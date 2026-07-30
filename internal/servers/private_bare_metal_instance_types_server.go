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

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivateBareMetalInstanceTypesServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.BareMetalInstanceTypesServer = (*PrivateBareMetalInstanceTypesServer)(nil)

type PrivateBareMetalInstanceTypesServer struct {
	privatev1.UnimplementedBareMetalInstanceTypesServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.BareMetalInstanceType]
}

func NewPrivateBareMetalInstanceTypesServer() *PrivateBareMetalInstanceTypesServerBuilder {
	return &PrivateBareMetalInstanceTypesServerBuilder{}
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetLogger(value *slog.Logger) *PrivateBareMetalInstanceTypesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetNotifier(value events.Notifier) *PrivateBareMetalInstanceTypesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateBareMetalInstanceTypesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateBareMetalInstanceTypesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateBareMetalInstanceTypesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateBareMetalInstanceTypesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) Build() (result *PrivateBareMetalInstanceTypesServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.BareMetalInstanceType]().
		SetLogger(b.logger).
		SetService(privatev1.BareMetalInstanceTypes_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateBareMetalInstanceTypesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateBareMetalInstanceTypesServer) List(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesListRequest) (response *privatev1.BareMetalInstanceTypesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Get(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesGetRequest) (response *privatev1.BareMetalInstanceTypesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Create(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesCreateRequest) (response *privatev1.BareMetalInstanceTypesCreateResponse, err error) {
	if request.GetObject() == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}

	spec := request.GetObject().GetSpec()
	if spec == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object spec is mandatory")
		return
	}

	// Validate required spec fields:
	hardware := spec.GetHardware()
	if hardware == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware' is mandatory")
		return
	}

	// Validate CPU specifications:
	cpu := hardware.GetCpu()
	if cpu == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.cpu' is mandatory")
		return
	}
	if cpu.GetCores() <= 0 {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.cpu.cores' must be greater than zero")
		return
	}
	if cpu.GetArchitecture() == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.cpu.architecture' is mandatory")
		return
	}
	if cpu.GetThreadsPerCore() <= 0 {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.cpu.threads_per_core' must be greater than zero")
		return
	}

	// Validate memory specifications:
	memory := hardware.GetMemory()
	if memory == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.memory' is mandatory")
		return
	}
	if memory.GetTotalGb() <= 0 {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.memory.total_gb' must be greater than zero")
		return
	}

	// Validate disk specifications:
	for i, disk := range hardware.GetDisks() {
		if disk.GetType() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.disks[%d].type' is mandatory", i)
			return
		}
		if disk.GetCapacityGb() <= 0 {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.disks[%d].capacity_gb' must be greater than zero", i)
			return
		}
		if disk.GetInterface() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.disks[%d].interface' is mandatory", i)
			return
		}
	}

	// Validate accelerator specifications:
	for i, accelerator := range hardware.GetAccelerators() {
		if accelerator.GetType() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.accelerators[%d].type' is mandatory", i)
			return
		}
		if accelerator.GetModel() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.accelerators[%d].model' is mandatory", i)
			return
		}
		if accelerator.MemoryGb != nil && *accelerator.MemoryGb <= 0 {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.accelerators[%d].memory_gb' must be greater than zero", i)
			return
		}
	}

	// Validate network port specifications:
	for i, port := range hardware.GetNetworkPorts() {
		if port.GetRole() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.network_ports[%d].role' is mandatory", i)
			return
		}
		if port.GetType() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.network_ports[%d].type' is mandatory", i)
			return
		}
		if port.GetSpeed() == "" {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.network_ports[%d].speed' is mandatory", i)
			return
		}
	}

	// Validate network port name uniqueness:
	err = validateNetworkPortUniqueness(hardware)
	if err != nil {
		return
	}

	// Validate host label selector:
	hostLabelSelector := spec.GetHostLabelSelector()
	if hostLabelSelector == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.host_label_selector' is mandatory")
		return
	}
	if len(hostLabelSelector.GetMatchLabels()) == 0 {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.host_label_selector.match_labels' must contain at least one label pair")
		return
	}

	// Set id from metadata.name (name-as-primary-key per API conventions):
	request.GetObject().SetId(request.GetObject().GetMetadata().GetName())

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Update(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesUpdateRequest) (response *privatev1.BareMetalInstanceTypesUpdateResponse, err error) {
	// Get the object identifier:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Fetch the existing object:
	getRequest := &privatev1.BareMetalInstanceTypesGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.BareMetalInstanceTypesGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existing := getResponse.GetObject()

	// Merge the update into a clone of the existing object:
	merged := cloneBareMetalInstanceType(existing)
	applyBareMetalInstanceTypeUpdate(merged, request.GetObject(), request.GetUpdateMask())

	// Validate immutable fields:
	err = validateBareMetalInstanceTypeImmutability(merged, existing)
	if err != nil {
		return
	}

	// Validate network port uniqueness on the merged object:
	err = validateNetworkPortUniqueness(merged.GetSpec().GetHardware())
	if err != nil {
		return
	}

	// Set the merged spec back into the request for the generic update:
	request.GetObject().SetSpec(merged.GetSpec())

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Delete(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesDeleteRequest) (response *privatev1.BareMetalInstanceTypesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Signal(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesSignalRequest) (response *privatev1.BareMetalInstanceTypesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// cloneBareMetalInstanceType creates a deep copy of a BareMetalInstanceType.
func cloneBareMetalInstanceType(bmt *privatev1.BareMetalInstanceType) *privatev1.BareMetalInstanceType {
	return proto.Clone(bmt).(*privatev1.BareMetalInstanceType)
}

// applyBareMetalInstanceTypeUpdate applies the update fields onto the base object, respecting the field mask.
// If no mask is provided, all fields from the update are applied.
// Field mask paths use the spec prefix (e.g., "spec.description", "spec.hardware") per API conventions.
func applyBareMetalInstanceTypeUpdate(base, update *privatev1.BareMetalInstanceType, mask *fieldmaskpb.FieldMask) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		proto.Merge(base, update)
		return
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "spec.description":
			base.GetSpec().SetDescription(update.GetSpec().GetDescription())
		case "spec.hardware":
			base.GetSpec().SetHardware(update.GetSpec().GetHardware())
		case "spec.host_label_selector":
			base.GetSpec().SetHostLabelSelector(update.GetSpec().GetHostLabelSelector())
		default:
			// For unknown paths, fall through - the generic handler will
			// reject invalid paths if needed.
		}
	}
}

// validateBareMetalInstanceTypeImmutability checks that immutable fields have not been changed.
// Core hardware specifications (CPU cores, memory total_gb) are immutable after creation
// following the same pattern as PrivateInstanceTypesServer for consistency.
func validateBareMetalInstanceTypeImmutability(merged, existing *privatev1.BareMetalInstanceType) error {
	// Validate immutable metadata fields:
	if merged.GetMetadata().GetName() != existing.GetMetadata().GetName() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'name' is immutable and cannot be changed from '%s' to '%s'",
			existing.GetMetadata().GetName(), merged.GetMetadata().GetName())
	}

	// Validate immutable hardware specs (core hardware configuration):
	existingHw := existing.GetSpec().GetHardware()
	mergedHw := merged.GetSpec().GetHardware()

	// Reject clearing hardware entirely:
	if existingHw != nil && mergedHw == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.hardware' is immutable and cannot be cleared")
	}

	// Skip validation if no existing hardware (new resource):
	if existingHw == nil {
		return nil
	}

	// CPU validation:
	existingCpu := existingHw.GetCpu()
	mergedCpu := mergedHw.GetCpu()
	if existingCpu != nil {
		if mergedCpu == nil {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.hardware.cpu' is immutable and cannot be cleared")
		}
		if mergedCpu.GetCores() != existingCpu.GetCores() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.hardware.cpu.cores' is immutable and cannot be changed from '%d' to '%d'",
				existingCpu.GetCores(), mergedCpu.GetCores())
		}
		if mergedCpu.GetArchitecture() != existingCpu.GetArchitecture() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.hardware.cpu.architecture' is immutable and cannot be changed from '%s' to '%s'",
				existingCpu.GetArchitecture(), mergedCpu.GetArchitecture())
		}
		if mergedCpu.GetThreadsPerCore() != existingCpu.GetThreadsPerCore() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.hardware.cpu.threads_per_core' is immutable and cannot be changed from '%d' to '%d'",
				existingCpu.GetThreadsPerCore(), mergedCpu.GetThreadsPerCore())
		}
	}

	// Memory validation:
	existingMem := existingHw.GetMemory()
	mergedMem := mergedHw.GetMemory()
	if existingMem != nil {
		if mergedMem == nil {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.hardware.memory' is immutable and cannot be cleared")
		}
		if mergedMem.GetTotalGb() != existingMem.GetTotalGb() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.hardware.memory.total_gb' is immutable and cannot be changed from '%d' to '%d'",
				existingMem.GetTotalGb(), mergedMem.GetTotalGb())
		}
	}

	return nil
}

// validateNetworkPortUniqueness validates that network port names are unique within the hardware specification.
func validateNetworkPortUniqueness(hardware *privatev1.BareMetalHardwareSpec) error {
	if hardware == nil {
		return nil
	}

	portNames := make(map[string]struct{})
	for i, port := range hardware.GetNetworkPorts() {
		if port.GetName() == "" {
			return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.network_ports[%d].name' is mandatory", i)
		}
		// Check for duplicate port names:
		if _, exists := portNames[port.GetName()]; exists {
			return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.hardware.network_ports[%d].name' has duplicate value '%s' - port names must be unique", i, port.GetName())
		}
		portNames[port.GetName()] = struct{}{}
	}
	return nil
}
