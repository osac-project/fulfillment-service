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

type PrivateHubsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.HubsServer = (*PrivateHubsServer)(nil)

type PrivateHubsServer struct {
	privatev1.UnimplementedHubsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.Hub]
}

func NewPrivateHubsServer() *PrivateHubsServerBuilder {
	return &PrivateHubsServerBuilder{}
}

func (b *PrivateHubsServerBuilder) SetLogger(value *slog.Logger) *PrivateHubsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateHubsServerBuilder) SetNotifier(value *database.Notifier) *PrivateHubsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateHubsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateHubsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateHubsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateHubsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateHubsServerBuilder) Build() (result *PrivateHubsServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.Hub]().
		SetLogger(b.logger).
		SetService(privatev1.Hubs_ServiceDesc.ServiceName).
		SetTable("hubs").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateHubsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateHubsServer) List(ctx context.Context,
	request *privatev1.HubsListRequest) (response *privatev1.HubsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateHubsServer) Get(ctx context.Context,
	request *privatev1.HubsGetRequest) (response *privatev1.HubsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateHubsServer) Create(ctx context.Context,
	request *privatev1.HubsCreateRequest) (response *privatev1.HubsCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateHubsServer) Update(ctx context.Context,
	request *privatev1.HubsUpdateRequest) (response *privatev1.HubsUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateHubsServer) Delete(ctx context.Context,
	request *privatev1.HubsDeleteRequest) (response *privatev1.HubsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateHubsServer) Signal(ctx context.Context,
	request *privatev1.HubsSignalRequest) (response *privatev1.HubsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
