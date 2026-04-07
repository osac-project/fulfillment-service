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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

// PrivateLeasesServerBuilder contains the data and logic needed to create a PrivateLeasesServer.
type PrivateLeasesServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.LeasesServer = (*PrivateLeasesServer)(nil)

// PrivateLeasesServer implements the private Leases gRPC service. It delegates all operations to a GenericServer.
type PrivateLeasesServer struct {
	privatev1.UnimplementedLeasesServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.Lease]
}

// NewPrivateLeasesServer creates a builder that can then be used to configure and create a PrivateLeasesServer.
func NewPrivateLeasesServer() *PrivateLeasesServerBuilder {
	return &PrivateLeasesServerBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *PrivateLeasesServerBuilder) SetLogger(value *slog.Logger) *PrivateLeasesServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier that will be used to send change notifications.
func (b *PrivateLeasesServerBuilder) SetNotifier(value *database.Notifier) *PrivateLeasesServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the logic that will be used to determine the creators for objects.
func (b *PrivateLeasesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateLeasesServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic that will be used to determine the tenants for objects.
func (b *PrivateLeasesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateLeasesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateLeasesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateLeasesServerBuilder {
	b.metricsRegisterer = value
	return b
}

// Build uses the configuration stored in the builder to create a new PrivateLeasesServer.
func (b *PrivateLeasesServerBuilder) Build() (result *PrivateLeasesServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.Lease]().
		SetLogger(b.logger).
		SetService(privatev1.Leases_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateLeasesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateLeasesServer) List(ctx context.Context,
	request *privatev1.LeasesListRequest) (response *privatev1.LeasesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateLeasesServer) Get(ctx context.Context,
	request *privatev1.LeasesGetRequest) (response *privatev1.LeasesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateLeasesServer) Create(ctx context.Context,
	request *privatev1.LeasesCreateRequest) (response *privatev1.LeasesCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateLeasesServer) Update(ctx context.Context,
	request *privatev1.LeasesUpdateRequest) (response *privatev1.LeasesUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateLeasesServer) Delete(ctx context.Context,
	request *privatev1.LeasesDeleteRequest) (response *privatev1.LeasesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateLeasesServer) Signal(ctx context.Context,
	request *privatev1.LeasesSignalRequest) (response *privatev1.LeasesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
