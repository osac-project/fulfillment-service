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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/utils"
)

type PrivateComputeInstancesServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.ComputeInstancesServer = (*PrivateComputeInstancesServer)(nil)

type PrivateComputeInstancesServer struct {
	privatev1.UnimplementedComputeInstancesServer

	logger       *slog.Logger
	generic      *GenericServer[*privatev1.ComputeInstance]
	templatesDao *dao.GenericDAO[*privatev1.ComputeInstanceTemplate]
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
		SetTable("compute_instance_templates").
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.ComputeInstance]().
		SetLogger(b.logger).
		SetService(privatev1.ComputeInstances_ServiceDesc.ServiceName).
		SetTable("compute_instances").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateComputeInstancesServer{
		logger:       b.logger,
		generic:      generic,
		templatesDao: templatesDao,
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
	// Validate template:
	err = s.validateTemplate(ctx, request.GetObject())
	if err != nil {
		return
	}

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateComputeInstancesServer) Update(ctx context.Context,
	request *privatev1.ComputeInstancesUpdateRequest) (response *privatev1.ComputeInstancesUpdateResponse, err error) {
	// Validate template:
	err = s.validateTemplate(ctx, request.GetObject())
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

// validateTemplate validates the template ID and parameters in the compute instance spec.
func (s *PrivateComputeInstancesServer) validateTemplate(ctx context.Context, vm *privatev1.ComputeInstance) error {
	if vm == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance is mandatory")
	}

	spec := vm.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance spec is mandatory")
	}

	templateID := spec.GetTemplate()
	if templateID == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "template ID is mandatory")
	}

	// Get the template:
	getTemplateResponse, err := s.templatesDao.Get().
		SetId(templateID).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Template retrieval failed",
			slog.String("template_id", templateID),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to retrieve template '%s'",
			templateID,
		)
	}
	template := getTemplateResponse.GetObject()
	if template == nil {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"template '%s' does not exist",
			templateID,
		)
	}

	// Validate template parameters:
	vmParameters := spec.GetTemplateParameters()
	err = utils.ValidateComputeInstanceTemplateParameters(template, vmParameters)
	if err != nil {
		return err
	}

	// Set default values for template parameters:
	actualVmParameters := utils.ProcessTemplateParametersWithDefaults(
		utils.ComputeInstanceTemplateAdapter{ComputeInstanceTemplate: template},
		vmParameters,
	)
	spec.SetTemplateParameters(actualVmParameters)

	return nil
}
