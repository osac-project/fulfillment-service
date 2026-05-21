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
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivatePublicIPAttachmentsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.PublicIPAttachmentsServer = (*PrivatePublicIPAttachmentsServer)(nil)

type PrivatePublicIPAttachmentsServer struct {
	privatev1.UnimplementedPublicIPAttachmentsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.PublicIPAttachment]
}

func NewPrivatePublicIPAttachmentsServer() *PrivatePublicIPAttachmentsServerBuilder {
	return &PrivatePublicIPAttachmentsServerBuilder{}
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetLogger(value *slog.Logger) *PrivatePublicIPAttachmentsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetNotifier(value events.Notifier) *PrivatePublicIPAttachmentsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivatePublicIPAttachmentsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivatePublicIPAttachmentsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivatePublicIPAttachmentsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) Build() (result *PrivatePublicIPAttachmentsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	generic, err := NewGenericServer[*privatev1.PublicIPAttachment]().
		SetLogger(b.logger).
		SetService(privatev1.PublicIPAttachments_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	result = &PrivatePublicIPAttachmentsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivatePublicIPAttachmentsServer) List(ctx context.Context,
	request *privatev1.PublicIPAttachmentsListRequest) (response *privatev1.PublicIPAttachmentsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) Get(ctx context.Context,
	request *privatev1.PublicIPAttachmentsGetRequest) (response *privatev1.PublicIPAttachmentsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) Create(ctx context.Context,
	request *privatev1.PublicIPAttachmentsCreateRequest) (response *privatev1.PublicIPAttachmentsCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) Update(ctx context.Context,
	request *privatev1.PublicIPAttachmentsUpdateRequest) (response *privatev1.PublicIPAttachmentsUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) Delete(ctx context.Context,
	request *privatev1.PublicIPAttachmentsDeleteRequest) (response *privatev1.PublicIPAttachmentsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) Signal(ctx context.Context,
	request *privatev1.PublicIPAttachmentsSignalRequest) (response *privatev1.PublicIPAttachmentsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
