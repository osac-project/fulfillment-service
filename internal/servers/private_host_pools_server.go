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

type PrivateHostPoolsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.HostPoolsServer = (*PrivateHostPoolsServer)(nil)

type PrivateHostPoolsServer struct {
	privatev1.UnimplementedHostPoolsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.HostPool]
}

func NewPrivateHostPoolsServer() *PrivateHostPoolsServerBuilder {
	return &PrivateHostPoolsServerBuilder{}
}

func (b *PrivateHostPoolsServerBuilder) SetLogger(value *slog.Logger) *PrivateHostPoolsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateHostPoolsServerBuilder) SetNotifier(value *database.Notifier) *PrivateHostPoolsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateHostPoolsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateHostPoolsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateHostPoolsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateHostPoolsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateHostPoolsServerBuilder) Build() (result *PrivateHostPoolsServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.HostPool]().
		SetLogger(b.logger).
		SetService(privatev1.HostPools_ServiceDesc.ServiceName).
		SetTable("host_pools").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateHostPoolsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateHostPoolsServer) List(ctx context.Context,
	request *privatev1.HostPoolsListRequest) (response *privatev1.HostPoolsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateHostPoolsServer) Get(ctx context.Context,
	request *privatev1.HostPoolsGetRequest) (response *privatev1.HostPoolsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateHostPoolsServer) Create(ctx context.Context,
	request *privatev1.HostPoolsCreateRequest) (response *privatev1.HostPoolsCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateHostPoolsServer) Update(ctx context.Context,
	request *privatev1.HostPoolsUpdateRequest) (response *privatev1.HostPoolsUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateHostPoolsServer) Delete(ctx context.Context,
	request *privatev1.HostPoolsDeleteRequest) (response *privatev1.HostPoolsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateHostPoolsServer) Signal(ctx context.Context,
	request *privatev1.HostPoolsSignalRequest) (response *privatev1.HostPoolsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
