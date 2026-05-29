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
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

type PrivateProjectsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
	idpClient         idp.Client
}

var _ privatev1.ProjectsServer = (*PrivateProjectsServer)(nil)

type PrivateProjectsServer struct {
	privatev1.UnimplementedProjectsServer
	logger    *slog.Logger
	generic   *GenericServer[*privatev1.Project]
	dao       *dao.GenericDAO[*privatev1.Project]
	idpClient idp.Client
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

func (b *PrivateProjectsServerBuilder) SetIdpClient(value idp.Client) *PrivateProjectsServerBuilder {
	b.idpClient = value
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
		logger:    b.logger,
		generic:   generic,
		dao:       dao,
		idpClient: b.idpClient,
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

func (s *PrivateProjectsServer) GrantAccess(ctx context.Context,
	request *privatev1.ProjectsGrantAccessRequest) (response *privatev1.ProjectsGrantAccessResponse, err error) {
	// Check if IDP client is configured
	if s.idpClient == nil {
		return nil, errors.New("IDP client not configured - cannot grant access")
	}

	// Get the project to extract tenant and name
	getRequest := &privatev1.ProjectsGetRequest{}
	getRequest.SetId(request.GetProjectId())

	getResponse, err := s.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}

	project := getResponse.GetObject()
	tenant := project.GetMetadata().GetTenant()
	projectName := project.GetMetadata().GetName()

	// Validate scope
	scope := request.GetScope()
	if scope != idp.ScopeViewProject && scope != idp.ScopeManageProject {
		return nil, errors.New("invalid scope: must be VIEW_PROJECT or MANAGE_PROJECT")
	}

	// Determine group path based on scope
	var groupPath string
	if scope == idp.ScopeViewProject {
		groupPath = fmt.Sprintf("/project-%s-%s-viewers", tenant, projectName)
	} else {
		groupPath = fmt.Sprintf("/project-%s-%s-managers", tenant, projectName)
	}

	// Look up user by username to get user ID
	// Note: This assumes username is the Keycloak user ID. In a real implementation,
	// you'd need to query Keycloak to get the user ID from username.
	// For now, we'll assume the username passed is already the user ID.
	userID := request.GetUsername()

	// Add user to the authorization group
	err = s.idpClient.AddUserToAuthorizationGroup(ctx, userID, groupPath)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to grant user access to project",
			slog.String("project_id", request.GetProjectId()),
			slog.String("user_id", userID),
			slog.String("scope", scope),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("failed to grant access: %w", err)
	}

	s.logger.InfoContext(ctx, "Granted user access to project",
		slog.String("project_id", request.GetProjectId()),
		slog.String("user_id", userID),
		slog.String("scope", scope),
	)

	response = &privatev1.ProjectsGrantAccessResponse{}
	return
}

func (s *PrivateProjectsServer) RevokeAccess(ctx context.Context,
	request *privatev1.ProjectsRevokeAccessRequest) (response *privatev1.ProjectsRevokeAccessResponse, err error) {
	// Check if IDP client is configured
	if s.idpClient == nil {
		return nil, errors.New("IDP client not configured - cannot revoke access")
	}

	// Get the project to extract tenant and name
	getRequest := &privatev1.ProjectsGetRequest{}
	getRequest.SetId(request.GetProjectId())

	getResponse, err := s.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}

	project := getResponse.GetObject()
	tenant := project.GetMetadata().GetTenant()
	projectName := project.GetMetadata().GetName()

	// Validate scope
	scope := request.GetScope()
	if scope != idp.ScopeViewProject && scope != idp.ScopeManageProject {
		return nil, errors.New("invalid scope: must be VIEW_PROJECT or MANAGE_PROJECT")
	}

	// Determine group path based on scope
	var groupPath string
	if scope == idp.ScopeViewProject {
		groupPath = fmt.Sprintf("/project-%s-%s-viewers", tenant, projectName)
	} else {
		groupPath = fmt.Sprintf("/project-%s-%s-managers", tenant, projectName)
	}

	// Look up user by username to get user ID
	userID := request.GetUsername()

	// Remove user from the authorization group
	err = s.idpClient.RemoveUserFromAuthorizationGroup(ctx, userID, groupPath)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to revoke user access to project",
			slog.String("project_id", request.GetProjectId()),
			slog.String("user_id", userID),
			slog.String("scope", scope),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("failed to revoke access: %w", err)
	}

	s.logger.InfoContext(ctx, "Revoked user access to project",
		slog.String("project_id", request.GetProjectId()),
		slog.String("user_id", userID),
		slog.String("scope", scope),
	)

	response = &privatev1.ProjectsRevokeAccessResponse{}
	return
}
