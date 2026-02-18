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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type PrivateComputeInstanceTemplatesServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.ComputeInstanceTemplatesServer = (*PrivateComputeInstanceTemplatesServer)(nil)

type PrivateComputeInstanceTemplatesServer struct {
	privatev1.UnimplementedComputeInstanceTemplatesServer
	logger  *slog.Logger
	generic *GenericServer[*privatev1.ComputeInstanceTemplate]
}

func NewPrivateComputeInstanceTemplatesServer() *PrivateComputeInstanceTemplatesServerBuilder {
	return &PrivateComputeInstanceTemplatesServerBuilder{}
}

func (b *PrivateComputeInstanceTemplatesServerBuilder) SetLogger(value *slog.Logger) *PrivateComputeInstanceTemplatesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateComputeInstanceTemplatesServerBuilder) SetNotifier(value *database.Notifier) *PrivateComputeInstanceTemplatesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateComputeInstanceTemplatesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateComputeInstanceTemplatesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateComputeInstanceTemplatesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateComputeInstanceTemplatesServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateComputeInstanceTemplatesServerBuilder) Build() (result *PrivateComputeInstanceTemplatesServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.ComputeInstanceTemplate]().
		SetLogger(b.logger).
		SetService(privatev1.ComputeInstanceTemplates_ServiceDesc.ServiceName).
		SetTable("compute_instance_templates").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateComputeInstanceTemplatesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateComputeInstanceTemplatesServer) List(ctx context.Context,
	request *privatev1.ComputeInstanceTemplatesListRequest) (response *privatev1.ComputeInstanceTemplatesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceTemplatesServer) Get(ctx context.Context,
	request *privatev1.ComputeInstanceTemplatesGetRequest) (response *privatev1.ComputeInstanceTemplatesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceTemplatesServer) Create(ctx context.Context,
	request *privatev1.ComputeInstanceTemplatesCreateRequest) (response *privatev1.ComputeInstanceTemplatesCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceTemplatesServer) Update(ctx context.Context,
	request *privatev1.ComputeInstanceTemplatesUpdateRequest) (response *privatev1.ComputeInstanceTemplatesUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceTemplatesServer) Delete(ctx context.Context,
	request *privatev1.ComputeInstanceTemplatesDeleteRequest) (response *privatev1.ComputeInstanceTemplatesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceTemplatesServer) Signal(ctx context.Context,
	request *privatev1.ComputeInstanceTemplatesSignalRequest) (response *privatev1.ComputeInstanceTemplatesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
