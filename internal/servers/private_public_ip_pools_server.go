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
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

// PrivatePublicIPPoolsServerBuilder contains the data and logic needed to create a new private public IP pools server.
type PrivatePublicIPPoolsServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.PublicIPPoolsServer = (*PrivatePublicIPPoolsServer)(nil)

// PrivatePublicIPPoolsServer is the private server for public IP pools.
type PrivatePublicIPPoolsServer struct {
	privatev1.UnimplementedPublicIPPoolsServer

	logger      *slog.Logger
	generic     *GenericServer[*privatev1.PublicIPPool]
	publicIPDAO *dao.GenericDAO[*privatev1.PublicIP]
}

// NewPrivatePublicIPPoolsServer creates a builder that can then be used to configure and create a new private public
// IP pools server.
func NewPrivatePublicIPPoolsServer() *PrivatePublicIPPoolsServerBuilder {
	return &PrivatePublicIPPoolsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *PrivatePublicIPPoolsServerBuilder) SetLogger(value *slog.Logger) *PrivatePublicIPPoolsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier used to publish change events.
func (b *PrivatePublicIPPoolsServerBuilder) SetNotifier(value *database.Notifier) *PrivatePublicIPPoolsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic used to determine the creators of objects.
func (b *PrivatePublicIPPoolsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivatePublicIPPoolsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic used to determine the tenants of objects.
func (b *PrivatePublicIPPoolsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivatePublicIPPoolsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register metrics for the underlying database access
// objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivatePublicIPPoolsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivatePublicIPPoolsServerBuilder {
	b.metricsRegisterer = value
	return b
}

// Build uses the data stored in the builder to create a new private public IP pools server.
func (b *PrivatePublicIPPoolsServerBuilder) Build() (result *PrivatePublicIPPoolsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the PublicIP DAO used to check for allocated IPs on pool deletion:
	publicIPDAO, err := dao.NewGenericDAO[*privatev1.PublicIP]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create public IP DAO: %w", err)
		return
	}

	generic, err := NewGenericServer[*privatev1.PublicIPPool]().
		SetLogger(b.logger).
		SetService(privatev1.PublicIPPools_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	result = &PrivatePublicIPPoolsServer{
		logger:      b.logger,
		generic:     generic,
		publicIPDAO: publicIPDAO,
	}
	return
}

func (s *PrivatePublicIPPoolsServer) List(ctx context.Context,
	request *privatev1.PublicIPPoolsListRequest) (response *privatev1.PublicIPPoolsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Get(ctx context.Context,
	request *privatev1.PublicIPPoolsGetRequest) (response *privatev1.PublicIPPoolsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Create(ctx context.Context,
	request *privatev1.PublicIPPoolsCreateRequest) (response *privatev1.PublicIPPoolsCreateResponse, err error) {
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Update(ctx context.Context,
	request *privatev1.PublicIPPoolsUpdateRequest) (response *privatev1.PublicIPPoolsUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivatePublicIPPoolsServer) Delete(ctx context.Context,
	request *privatev1.PublicIPPoolsDeleteRequest) (response *privatev1.PublicIPPoolsDeleteResponse, err error) {
	if s.publicIPDAO != nil {
		err = s.checkNoAllocatedIPs(ctx, request.GetId())
		if err != nil {
			return
		}
	}
	err = s.generic.Delete(ctx, request, &response)
	return
}

// checkNoAllocatedIPs returns a FailedPrecondition error when at least one PublicIP still
// references this pool. Any database error is treated as a hard failure so that a transient
// connectivity issue cannot silently bypass the referential-integrity check and orphan IPs.
func (s *PrivatePublicIPPoolsServer) checkNoAllocatedIPs(ctx context.Context, poolID string) error {
	filter := fmt.Sprintf("this.spec.pool == %q", poolID)
	listResponse, err := s.publicIPDAO.List().
		SetFilter(filter).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to verify allocated public IPs for pool",
			slog.String("pool_id", poolID),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to verify allocated public IPs for pool '%s'",
			poolID,
		)
	}
	if total := listResponse.GetTotal(); total > 0 {
		return grpcstatus.Errorf(
			grpccodes.FailedPrecondition,
			"cannot delete public IP pool '%s': %d public IP(s) are still allocated from it",
			poolID, total,
		)
	}
	return nil
}

func (s *PrivatePublicIPPoolsServer) Signal(ctx context.Context,
	request *privatev1.PublicIPPoolsSignalRequest) (response *privatev1.PublicIPPoolsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
