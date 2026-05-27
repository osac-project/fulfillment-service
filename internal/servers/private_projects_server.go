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

	"github.com/prometheus/client_golang/prometheus"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivateProjectsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.ProjectsServer = (*PrivateProjectsServer)(nil)

type PrivateProjectsServer struct {
	privatev1.UnimplementedProjectsServer
	logger  *slog.Logger
	generic *GenericServer[*privatev1.Project]
	dao     *dao.GenericDAO[*privatev1.Project]
}

func NewPrivateProjectsServer() *PrivateProjectsServerBuilder {
	return &PrivateProjectsServerBuilder{}
}

func (b *PrivateProjectsServerBuilder) SetLogger(value *slog.Logger) *PrivateProjectsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateProjectsServerBuilder) SetNotifier(value events.Notifier) *PrivateProjectsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateProjectsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateProjectsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateProjectsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateProjectsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateProjectsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateProjectsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateProjectsServerBuilder) Build() (result *PrivateProjectsServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.Project]().
		SetLogger(b.logger).
		SetService(privatev1.Projects_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the DAO:
	dao, err := dao.NewGenericDAO[*privatev1.Project]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateProjectsServer{
		logger:  b.logger,
		generic: generic,
		dao:     dao,
	}
	return
}

func (s *PrivateProjectsServer) List(ctx context.Context,
	request *privatev1.ProjectsListRequest) (response *privatev1.ProjectsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateProjectsServer) Get(ctx context.Context,
	request *privatev1.ProjectsGetRequest) (response *privatev1.ProjectsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateProjectsServer) Create(ctx context.Context,
	request *privatev1.ProjectsCreateRequest) (response *privatev1.ProjectsCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateProjectsServer) Update(ctx context.Context,
	request *privatev1.ProjectsUpdateRequest) (response *privatev1.ProjectsUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateProjectsServer) Delete(ctx context.Context,
	request *privatev1.ProjectsDeleteRequest) (response *privatev1.ProjectsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateProjectsServer) Signal(ctx context.Context,
	request *privatev1.ProjectsSignalRequest) (response *privatev1.ProjectsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
