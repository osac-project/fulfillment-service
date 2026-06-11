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
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

// PublicIPPoolsServerBuilder contains the data and logic needed to create a new public IP pools server.
type PublicIPPoolsServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.PublicIPPoolsServer = (*PublicIPPoolsServer)(nil)

// PublicIPPoolsServer is the read-only public server for public IP pools.
type PublicIPPoolsServer struct {
	publicv1.UnimplementedPublicIPPoolsServer

	logger    *slog.Logger
	delegate  privatev1.PublicIPPoolsServer
	outMapper *GenericMapper[*privatev1.PublicIPPool, *publicv1.PublicIPPool]
}

// NewPublicIPPoolsServer creates a builder that can then be used to configure and create a new public IP pools server.
func NewPublicIPPoolsServer() *PublicIPPoolsServerBuilder {
	return &PublicIPPoolsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *PublicIPPoolsServerBuilder) SetLogger(value *slog.Logger) *PublicIPPoolsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *PublicIPPoolsServerBuilder) SetNotifier(value *database.Notifier) *PublicIPPoolsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is mandatory.
func (b *PublicIPPoolsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PublicIPPoolsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *PublicIPPoolsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PublicIPPoolsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register metrics for the underlying database access
// objects. This is optional. If not set, no metrics will be recorded.
func (b *PublicIPPoolsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PublicIPPoolsServerBuilder {
	b.metricsRegisterer = value
	return b
}

// Build uses the data stored in the builder to create a new public IP pools server.
func (b *PublicIPPoolsServerBuilder) Build() (result *PublicIPPoolsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.attributionLogic == nil {
		err = errors.New("attribution logic is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// SetStrict(false): private type has extra fields (spec.implementation_strategy, status.*) not in public type.
	outMapper, err := NewGenericMapper[*privatev1.PublicIPPool, *publicv1.PublicIPPool]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	delegate, err := NewPrivatePublicIPPoolsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	result = &PublicIPPoolsServer{
		logger:    b.logger,
		delegate:  delegate,
		outMapper: outMapper,
	}
	return
}

// isPoolPubliclyVisible returns true when a pool should be exposed via the public API.
// A pool is visible when it is in the READY state and has available capacity.
func isPoolPubliclyVisible(p *privatev1.PublicIPPool) bool {
	return p.GetStatus().GetState() == privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY &&
		p.GetStatus().GetAvailable() > 0
}

func (s *PublicIPPoolsServer) List(ctx context.Context,
	request *publicv1.PublicIPPoolsListRequest) (response *publicv1.PublicIPPoolsListResponse, err error) {
	// Fetch all pools matching the user's filter from the private server without a limit, so that
	// the Go-level readiness filter below can compute the correct total before paginating.
	// Pool counts are small (admin-managed resource), so fetching all is acceptable.
	privateRequest := &privatev1.PublicIPPoolsListRequest{}
	privateRequest.SetFilter(request.GetFilter())

	privateResponse, err := s.delegate.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Retain only pools that are ready and have available capacity.
	// The CEL filter translator does not support enum fields, so this check must be done in Go.
	var eligible []*privatev1.PublicIPPool
	for _, p := range privateResponse.GetItems() {
		if isPoolPubliclyVisible(p) {
			eligible = append(eligible, p)
		}
	}

	// Apply offset and limit in Go after filtering.
	total := int32(len(eligible)) // #nosec G115 -- bounded by pool size
	offset := request.GetOffset()
	limit := request.GetLimit()
	if offset < 0 || limit < 0 {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "offset and limit must be non-negative")
	}
	if offset > total {
		offset = total
	}
	paged := eligible[offset:]
	if limit > 0 && int32(len(paged)) > limit { // #nosec G115 -- bounded by pool size
		paged = paged[:limit]
	}

	publicItems := make([]*publicv1.PublicIPPool, len(paged))
	for i, privateItem := range paged {
		publicItem := &publicv1.PublicIPPool{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map private public IP pool to public", slog.Any("error", err))
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP pools")
		}
		publicItems[i] = publicItem
	}

	response = &publicv1.PublicIPPoolsListResponse{}
	response.SetSize(int32(len(publicItems))) // #nosec G115 -- bounded by pool size
	response.SetTotal(total)
	response.SetItems(publicItems)
	return
}

func (s *PublicIPPoolsServer) Get(ctx context.Context,
	request *publicv1.PublicIPPoolsGetRequest) (response *publicv1.PublicIPPoolsGetResponse, err error) {
	privateRequest := &privatev1.PublicIPPoolsGetRequest{}
	privateRequest.SetId(request.GetId())

	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	pool := privateResponse.GetObject()
	if !isPoolPubliclyVisible(pool) {
		s.logger.DebugContext(ctx, "Pool not eligible for public API",
			slog.String("id", request.GetId()),
		)
		return nil, grpcstatus.Errorf(grpccodes.NotFound, "public IP pool not found")
	}

	publicPool := &publicv1.PublicIPPool{}
	err = s.outMapper.Copy(ctx, pool, publicPool)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map private public IP pool to public", slog.Any("error", err))
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP pool")
	}

	response = &publicv1.PublicIPPoolsGetResponse{}
	response.SetObject(publicPool)
	return
}
