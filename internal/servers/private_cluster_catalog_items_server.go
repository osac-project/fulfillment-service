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
	"google.golang.org/protobuf/reflect/protoreflect"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivateClusterCatalogItemsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
	filterDesc        protoreflect.MessageDescriptor
}

var _ privatev1.ClusterCatalogItemsServer = (*PrivateClusterCatalogItemsServer)(nil)

type PrivateClusterCatalogItemsServer struct {
	privatev1.UnimplementedClusterCatalogItemsServer
	logger  *slog.Logger
	generic *GenericServer[*privatev1.ClusterCatalogItem]
}

func NewPrivateClusterCatalogItemsServer() *PrivateClusterCatalogItemsServerBuilder {
	return &PrivateClusterCatalogItemsServerBuilder{}
}

func (b *PrivateClusterCatalogItemsServerBuilder) SetLogger(value *slog.Logger) *PrivateClusterCatalogItemsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateClusterCatalogItemsServerBuilder) SetNotifier(
	value events.Notifier) *PrivateClusterCatalogItemsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateClusterCatalogItemsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateClusterCatalogItemsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateClusterCatalogItemsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateClusterCatalogItemsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateClusterCatalogItemsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateClusterCatalogItemsServerBuilder {
	b.metricsRegisterer = value
	return b
}

// SetFilterDesc sets the protobuf message descriptor used to validate and translate CEL filter expressions. This is
// optional. When unset, the private object type is used. Public servers that wrap this private server should pass the
// corresponding public object descriptor so that clients cannot filter on private-only fields.
func (b *PrivateClusterCatalogItemsServerBuilder) SetFilterDesc(value protoreflect.MessageDescriptor) *PrivateClusterCatalogItemsServerBuilder {
	b.filterDesc = value
	return b
}

func (b *PrivateClusterCatalogItemsServerBuilder) Build() (result *PrivateClusterCatalogItemsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}
	generic, err := NewGenericServer[*privatev1.ClusterCatalogItem]().
		SetLogger(b.logger).
		SetService(privatev1.ClusterCatalogItems_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		SetFilterDesc(b.filterDesc).
		Build()
	if err != nil {
		return
	}

	result = &PrivateClusterCatalogItemsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateClusterCatalogItemsServer) List(ctx context.Context,
	request *privatev1.ClusterCatalogItemsListRequest) (response *privatev1.ClusterCatalogItemsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateClusterCatalogItemsServer) Get(ctx context.Context,
	request *privatev1.ClusterCatalogItemsGetRequest) (response *privatev1.ClusterCatalogItemsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateClusterCatalogItemsServer) Create(ctx context.Context,
	request *privatev1.ClusterCatalogItemsCreateRequest) (response *privatev1.ClusterCatalogItemsCreateResponse, err error) {
	if object := request.GetObject(); object != nil {
		if err = validateFieldDefinitions(object.GetFieldDefinitions()); err != nil {
			return
		}
	}
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateClusterCatalogItemsServer) Update(ctx context.Context,
	request *privatev1.ClusterCatalogItemsUpdateRequest) (response *privatev1.ClusterCatalogItemsUpdateResponse, err error) {
	if object := request.GetObject(); object != nil {
		if err = validateFieldDefinitions(object.GetFieldDefinitions()); err != nil {
			return
		}
	}
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateClusterCatalogItemsServer) Delete(ctx context.Context,
	request *privatev1.ClusterCatalogItemsDeleteRequest) (response *privatev1.ClusterCatalogItemsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateClusterCatalogItemsServer) Signal(ctx context.Context,
	request *privatev1.ClusterCatalogItemsSignalRequest) (response *privatev1.ClusterCatalogItemsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
