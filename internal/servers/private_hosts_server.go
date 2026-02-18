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

type PrivateHostsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.HostsServer = (*PrivateHostsServer)(nil)

type PrivateHostsServer struct {
	privatev1.UnimplementedHostsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.Host]
}

func NewPrivateHostsServer() *PrivateHostsServerBuilder {
	return &PrivateHostsServerBuilder{}
}

func (b *PrivateHostsServerBuilder) SetLogger(value *slog.Logger) *PrivateHostsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateHostsServerBuilder) SetNotifier(value *database.Notifier) *PrivateHostsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateHostsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateHostsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateHostsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateHostsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateHostsServerBuilder) Build() (result *PrivateHostsServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.Host]().
		SetLogger(b.logger).
		SetService(privatev1.Hosts_ServiceDesc.ServiceName).
		SetTable("hosts").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateHostsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateHostsServer) List(ctx context.Context,
	request *privatev1.HostsListRequest) (response *privatev1.HostsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateHostsServer) Get(ctx context.Context,
	request *privatev1.HostsGetRequest) (response *privatev1.HostsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateHostsServer) Create(ctx context.Context,
	request *privatev1.HostsCreateRequest) (response *privatev1.HostsCreateResponse, err error) {
	s.setDefaults(request.GetObject())
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateHostsServer) Update(ctx context.Context,
	request *privatev1.HostsUpdateRequest) (response *privatev1.HostsUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateHostsServer) Delete(ctx context.Context,
	request *privatev1.HostsDeleteRequest) (response *privatev1.HostsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateHostsServer) Signal(ctx context.Context,
	request *privatev1.HostsSignalRequest) (response *privatev1.HostsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

func (s *PrivateHostsServer) setDefaults(host *privatev1.Host) {
	if !host.HasStatus() {
		host.SetStatus(&privatev1.HostStatus{})
	}
}
