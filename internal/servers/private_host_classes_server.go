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

type PrivateHostClassesServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.HostClassesServer = (*PrivateHostClassesServer)(nil)

type PrivateHostClassesServer struct {
	privatev1.UnimplementedHostClassesServer
	logger  *slog.Logger
	generic *GenericServer[*privatev1.HostClass]
}

func NewPrivateHostClassesServer() *PrivateHostClassesServerBuilder {
	return &PrivateHostClassesServerBuilder{}
}

func (b *PrivateHostClassesServerBuilder) SetLogger(value *slog.Logger) *PrivateHostClassesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateHostClassesServerBuilder) SetNotifier(value *database.Notifier) *PrivateHostClassesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateHostClassesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateHostClassesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateHostClassesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateHostClassesServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateHostClassesServerBuilder) Build() (result *PrivateHostClassesServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.HostClass]().
		SetLogger(b.logger).
		SetService(privatev1.HostClasses_ServiceDesc.ServiceName).
		SetTable("host_classes").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateHostClassesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateHostClassesServer) List(ctx context.Context,
	request *privatev1.HostClassesListRequest) (response *privatev1.HostClassesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateHostClassesServer) Get(ctx context.Context,
	request *privatev1.HostClassesGetRequest) (response *privatev1.HostClassesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateHostClassesServer) Create(ctx context.Context,
	request *privatev1.HostClassesCreateRequest) (response *privatev1.HostClassesCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateHostClassesServer) Update(ctx context.Context,
	request *privatev1.HostClassesUpdateRequest) (response *privatev1.HostClassesUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateHostClassesServer) Delete(ctx context.Context,
	request *privatev1.HostClassesDeleteRequest) (response *privatev1.HostClassesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateHostClassesServer) Signal(ctx context.Context,
	request *privatev1.HostClassesSignalRequest) (response *privatev1.HostClassesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
