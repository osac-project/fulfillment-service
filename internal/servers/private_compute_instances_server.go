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
	"strings"

	"maps"

	"github.com/prometheus/client_golang/prometheus"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/utils"
)

type PrivateComputeInstancesServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.ComputeInstancesServer = (*PrivateComputeInstancesServer)(nil)

type PrivateComputeInstancesServer struct {
	privatev1.UnimplementedComputeInstancesServer

	logger            *slog.Logger
	generic           *GenericServer[*privatev1.ComputeInstance]
	templatesDao      *dao.GenericDAO[*privatev1.ComputeInstanceTemplate]
	subnetsDao        *dao.GenericDAO[*privatev1.Subnet]
	securityGroupsDao *dao.GenericDAO[*privatev1.SecurityGroup]
}

func NewPrivateComputeInstancesServer() *PrivateComputeInstancesServerBuilder {
	return &PrivateComputeInstancesServerBuilder{}
}

func (b *PrivateComputeInstancesServerBuilder) SetLogger(value *slog.Logger) *PrivateComputeInstancesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateComputeInstancesServerBuilder) SetNotifier(value *database.Notifier) *PrivateComputeInstancesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateComputeInstancesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateComputeInstancesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateComputeInstancesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateComputeInstancesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateComputeInstancesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateComputeInstancesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateComputeInstancesServerBuilder) Build() (result *PrivateComputeInstancesServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the templates DAO:
	templatesDao, err := dao.NewGenericDAO[*privatev1.ComputeInstanceTemplate]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the Subnets DAO for network validation:
	subnetsDao, err := dao.NewGenericDAO[*privatev1.Subnet]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the SecurityGroups DAO for network validation:
	securityGroupsDao, err := dao.NewGenericDAO[*privatev1.SecurityGroup]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.ComputeInstance]().
		SetLogger(b.logger).
		SetService(privatev1.ComputeInstances_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateComputeInstancesServer{
		logger:            b.logger,
		generic:           generic,
		templatesDao:      templatesDao,
		subnetsDao:        subnetsDao,
		securityGroupsDao: securityGroupsDao,
	}
	return
}

func (s *PrivateComputeInstancesServer) List(ctx context.Context,
	request *privatev1.ComputeInstancesListRequest) (response *privatev1.ComputeInstancesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateComputeInstancesServer) Get(ctx context.Context,
	request *privatev1.ComputeInstancesGetRequest) (response *privatev1.ComputeInstancesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateComputeInstancesServer) Create(ctx context.Context,
	request *privatev1.ComputeInstancesCreateRequest) (response *privatev1.ComputeInstancesCreateResponse, err error) {
	vm := request.GetObject()
	if vm == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance is mandatory")
		return
	}
	spec := vm.GetSpec()
	if spec == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance spec is mandatory")
		return
	}
	if strings.TrimSpace(spec.GetSubnet()) == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.subnet' is required")
		return
	}

	// Validate network references:
	err = s.validateNetworkReferences(ctx, vm)
	if err != nil {
		return
	}

	// Fetch and validate template:
	template, err := s.fetchAndValidateTemplate(ctx, vm)
	if err != nil {
		return
	}

	// Apply template spec defaults and validate that all required spec fields are present.
	err = s.applySpecDefaults(spec, template)
	if err != nil {
		return
	}

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateComputeInstancesServer) Update(ctx context.Context,
	request *privatev1.ComputeInstancesUpdateRequest) (response *privatev1.ComputeInstancesUpdateResponse, err error) {
	// Only validate fields affected by the update mask. With a field mask the object
	// is sparse so validating fields absent from it would fail incorrectly.
	mask := request.GetUpdateMask()
	if hasMaskPrefix(mask, "spec.subnet", "spec.security_groups") {
		err = s.validateNetworkReferences(ctx, request.GetObject())
		if err != nil {
			return
		}
	}
	err = s.validateTemplateImmutability(ctx, request)
	if err != nil {
		return
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateComputeInstancesServer) Delete(ctx context.Context,
	request *privatev1.ComputeInstancesDeleteRequest) (response *privatev1.ComputeInstancesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateComputeInstancesServer) Signal(ctx context.Context,
	request *privatev1.ComputeInstancesSignalRequest) (response *privatev1.ComputeInstancesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// fetchAndValidateTemplate fetches the template, validates parameters in the compute instance spec,
// applies template parameter defaults, and returns the template.
func (s *PrivateComputeInstancesServer) fetchAndValidateTemplate(ctx context.Context, vm *privatev1.ComputeInstance) (*privatev1.ComputeInstanceTemplate, error) {
	if vm == nil {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance is mandatory")
	}

	spec := vm.GetSpec()
	if spec == nil {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance spec is mandatory")
	}

	template, err := s.fetchTemplate(ctx, spec.GetTemplate())
	if err != nil {
		return nil, err
	}

	// Validate template parameters:
	vmParameters := spec.GetTemplateParameters()
	err = utils.ValidateComputeInstanceTemplateParameters(template, vmParameters)
	if err != nil {
		return nil, err
	}

	// Set default values for template parameters:
	actualVmParameters := utils.ProcessTemplateParametersWithDefaults(
		utils.ComputeInstanceTemplateAdapter{ComputeInstanceTemplate: template},
		vmParameters,
	)
	spec.SetTemplateParameters(actualVmParameters)

	return template, nil
}

// fetchTemplate fetches a compute instance template
func (s *PrivateComputeInstancesServer) fetchTemplate(ctx context.Context, templateID string) (*privatev1.ComputeInstanceTemplate, error) {
	if templateID == "" {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "template ID is mandatory")
	}

	getTemplateResponse, err := s.templatesDao.Get().
		SetId(templateID).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return nil, grpcstatus.Errorf(grpccodes.InvalidArgument,
				"template '%s' does not exist", templateID)
		}
		s.logger.ErrorContext(
			ctx,
			"Template retrieval failed",
			slog.String("template_id", templateID),
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to retrieve template '%s'",
			templateID,
		)
	}

	template := getTemplateResponse.GetObject()
	if template == nil {
		return nil, grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"template '%s' does not exist",
			templateID,
		)
	}
	return template, nil
}

// applySpecDefaults applies template spec defaults to the spec in place and validates
// that all required fields are present. User-provided values are never overridden.
func (s *PrivateComputeInstancesServer) applySpecDefaults(
	spec *privatev1.ComputeInstanceSpec,
	template *privatev1.ComputeInstanceTemplate,
) error {
	utils.ApplySpecDefaults(spec, template.GetSpecDefaults())
	return utils.ValidateRequiredSpecFields(spec)
}

// validateTemplateImmutability ensures that the template and template_parameters fields
// cannot be changed after compute instance creation.
func (s *PrivateComputeInstancesServer) validateTemplateImmutability(ctx context.Context,
	request *privatev1.ComputeInstancesUpdateRequest) error {
	updateMask := request.GetUpdateMask()
	updatingTemplate := hasMaskPrefix(updateMask, "spec.template")
	updatingTemplateParams := hasMaskPrefix(updateMask, "spec.template_parameters")

	if !updatingTemplate && !updatingTemplateParams {
		return nil
	}

	ci := request.GetObject()
	if ci == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance is mandatory")
	}
	id := ci.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance id is mandatory")
	}

	getResponse, err := s.generic.dao.Get().SetId(id).Do(ctx)
	if err != nil {
		return err
	}
	existingCI := getResponse.GetObject()

	existingSpec := existingCI.GetSpec()
	newSpec := request.GetObject().GetSpec()

	if updatingTemplate && existingSpec.GetTemplate() != newSpec.GetTemplate() {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"cannot change spec.template from '%s' to '%s': template is immutable",
			existingSpec.GetTemplate(),
			newSpec.GetTemplate(),
		)
	}

	if updatingTemplateParams {
		templateParamsEqual := func(first, second *anypb.Any) bool {
			return proto.Equal(first, second)
		}
		if !maps.EqualFunc(existingSpec.GetTemplateParameters(), newSpec.GetTemplateParameters(), templateParamsEqual) {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"cannot change spec.template_parameters: template parameters are immutable",
			)
		}
	}

	return nil
}

func hasMaskPrefix(mask *fieldmaskpb.FieldMask, prefixes ...string) bool {
	if mask == nil || len(mask.GetPaths()) == 0 {
		return true
	}
	for _, path := range mask.GetPaths() {
		for _, prefix := range prefixes {
			if path == prefix || strings.HasPrefix(path, prefix+".") {
				return true
			}
		}
	}
	return false
}

// validateNetworkReferences validates that referenced Subnet and SecurityGroups exist, are in READY state,
// belong to the same tenant, and SecurityGroups belong to the same VirtualNetwork as the Subnet.
//
// Implements requirements VAL-01, VAL-02, VAL-03, VAL-04.
func (s *PrivateComputeInstancesServer) validateNetworkReferences(ctx context.Context, vm *privatev1.ComputeInstance) error {
	if vm == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance is mandatory")
	}

	spec := vm.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance spec is mandatory")
	}

	subnetID := spec.GetSubnet()
	securityGroupIDs := spec.GetSecurityGroups()

	// If no network references, nothing to validate
	if subnetID == "" && len(securityGroupIDs) == 0 {
		return nil
	}

	var subnet *privatev1.Subnet
	var virtualNetworkID string

	// VAL-01: Validate Subnet exists and is READY
	if subnetID != "" {
		getSubnetResponse, err := s.subnetsDao.Get().
			SetId(subnetID).
			Do(ctx)
		if err != nil {
			var notFoundErr *dao.ErrNotFound
			if errors.As(err, &notFoundErr) {
				return grpcstatus.Errorf(grpccodes.InvalidArgument,
					"subnet '%s' does not exist", subnetID)
			}
			s.logger.ErrorContext(ctx, "Failed to query Subnet",
				slog.String("subnet_id", subnetID),
				slog.Any("error", err))
			return grpcstatus.Errorf(grpccodes.Internal, "failed to validate subnet")
		}

		subnet = getSubnetResponse.GetObject()
		if subnet == nil {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"subnet '%s' does not exist", subnetID)
		}

		// VAL-01: Check Subnet is READY
		if subnet.GetStatus().GetState() != privatev1.SubnetState_SUBNET_STATE_READY {
			return grpcstatus.Errorf(grpccodes.FailedPrecondition,
				"subnet '%s' is not in READY state (current state: %s)",
				subnetID, subnet.GetStatus().GetState().String())
		}

		// Store VirtualNetwork ID for SecurityGroup validation
		virtualNetworkID = subnet.GetSpec().GetVirtualNetwork()
	}

	// VAL-02, VAL-03: Validate SecurityGroups exist, are READY, and belong to same VirtualNetwork
	for _, sgID := range securityGroupIDs {
		if sgID == "" {
			continue // Skip empty strings
		}

		getSGResponse, err := s.securityGroupsDao.Get().
			SetId(sgID).
			Do(ctx)
		if err != nil {
			var notFoundErr *dao.ErrNotFound
			if errors.As(err, &notFoundErr) {
				return grpcstatus.Errorf(grpccodes.InvalidArgument,
					"security group '%s' does not exist", sgID)
			}
			s.logger.ErrorContext(ctx, "Failed to query SecurityGroup",
				slog.String("security_group_id", sgID),
				slog.Any("error", err))
			return grpcstatus.Errorf(grpccodes.Internal, "failed to validate security group")
		}

		sg := getSGResponse.GetObject()
		if sg == nil {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"security group '%s' does not exist", sgID)
		}

		// VAL-02: Check SecurityGroup is READY
		if sg.GetStatus().GetState() != privatev1.SecurityGroupState_SECURITY_GROUP_STATE_READY {
			return grpcstatus.Errorf(grpccodes.FailedPrecondition,
				"security group '%s' is not in READY state (current state: %s)",
				sgID, sg.GetStatus().GetState().String())
		}

		// VAL-03: If Subnet was provided, verify SecurityGroup belongs to same VirtualNetwork
		if virtualNetworkID != "" {
			sgVirtualNetworkID := sg.GetSpec().GetVirtualNetwork()
			if sgVirtualNetworkID != virtualNetworkID {
				return grpcstatus.Errorf(grpccodes.InvalidArgument,
					"security group '%s' belongs to VirtualNetwork '%s', but subnet '%s' belongs to VirtualNetwork '%s'",
					sgID, sgVirtualNetworkID, subnetID, virtualNetworkID)
			}
		}
	}

	// VAL-04: Tenant isolation is enforced by TenancyLogic in GenericDAO.Get()
	// All DAO lookups above are automatically scoped to the requesting tenant

	return nil
}
