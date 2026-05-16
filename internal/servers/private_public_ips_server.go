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
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

// validPublicIPTransitions defines the allowed state transitions for PublicIP resources.
// The key is the current state and the value is the list of valid target states.
var validPublicIPTransitions = map[privatev1.PublicIPState][]privatev1.PublicIPState{
	privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING:   {privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED},
	privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED: {privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED, privatev1.PublicIPState_PUBLIC_IP_STATE_DELETING},
	privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED:    {privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED},
}

// PrivatePublicIPsServerBuilder contains the data and logic needed to create a PrivatePublicIPsServer.
type PrivatePublicIPsServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.PublicIPsServer = (*PrivatePublicIPsServer)(nil)

// PrivatePublicIPsServer implements the private PublicIPs gRPC service. It delegates most operations to a
// GenericServer and adds pool validation on Create, state machine enforcement on Update, and delete
// constraints.
type PrivatePublicIPsServer struct {
	privatev1.UnimplementedPublicIPsServer

	logger          *slog.Logger
	generic         *GenericServer[*privatev1.PublicIP]
	publicIPPoolDao *dao.GenericDAO[*privatev1.PublicIPPool]
}

// NewPrivatePublicIPsServer creates a builder that can then be used to configure and create a PrivatePublicIPsServer.
func NewPrivatePublicIPsServer() *PrivatePublicIPsServerBuilder {
	return &PrivatePublicIPsServerBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *PrivatePublicIPsServerBuilder) SetLogger(value *slog.Logger) *PrivatePublicIPsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier that will be used to send change notifications.
func (b *PrivatePublicIPsServerBuilder) SetNotifier(value *database.Notifier) *PrivatePublicIPsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the logic that will be used to determine the creators for objects. This is mandatory.
func (b *PrivatePublicIPsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivatePublicIPsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic that will be used to determine the tenants for objects.
func (b *PrivatePublicIPsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivatePublicIPsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivatePublicIPsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivatePublicIPsServerBuilder {
	b.metricsRegisterer = value
	return b
}

// Build uses the configuration stored in the builder to create a new PrivatePublicIPsServer.
func (b *PrivatePublicIPsServerBuilder) Build() (result *PrivatePublicIPsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}
	if b.attributionLogic == nil {
		err = errors.New("attribution logic is mandatory")
		return
	}

	// Create the PublicIPPool DAO for pool validation and capacity tracking:
	publicIPPoolDao, err := dao.NewGenericDAO[*privatev1.PublicIPPool]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.PublicIP]().
		SetLogger(b.logger).
		SetService(privatev1.PublicIPs_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivatePublicIPsServer{
		logger:          b.logger,
		generic:         generic,
		publicIPPoolDao: publicIPPoolDao,
	}
	return
}

func (s *PrivatePublicIPsServer) List(ctx context.Context,
	request *privatev1.PublicIPsListRequest) (response *privatev1.PublicIPsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivatePublicIPsServer) Get(ctx context.Context,
	request *privatev1.PublicIPsGetRequest) (response *privatev1.PublicIPsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

// Create validates the pool reference, creates the PublicIP, and updates pool capacity counters.
// All operations share the gRPC interceptor's database transaction: if any step fails, the
// entire transaction rolls back (pool capacity, PublicIP creation, and validation are atomic).
func (s *PrivatePublicIPsServer) Create(ctx context.Context,
	request *privatev1.PublicIPsCreateRequest) (response *privatev1.PublicIPsCreateResponse, err error) {
	publicIP := request.GetObject()

	// Validate before creating:
	err = s.validatePublicIP(ctx, publicIP)
	if err != nil {
		return
	}

	// Validate pool reference (existence, READY state, capacity):
	poolID := publicIP.GetSpec().GetPool()
	err = s.validatePoolReference(ctx, poolID)
	if err != nil {
		return
	}

	// Set initial state to PENDING
	if publicIP.GetStatus() == nil {
		publicIP.SetStatus(privatev1.PublicIPStatus_builder{
			State: privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING,
		}.Build())
	} else {
		publicIP.GetStatus().SetState(privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)
	}

	// Create the PublicIP:
	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		return
	}

	// Update pool capacity: decrement available, increment allocated.
	// Concurrent allocations are serialized by the pool's resource_version
	// (optimistic locking). A version conflict returns Aborted for client retry.
	err = s.updatePoolCapacity(ctx, poolID, 1)
	if err != nil {
		return
	}

	return
}

// Update validates pool immutability and state machine transitions before persisting.
// The Get and Update share the gRPC interceptor's transaction. GenericServer.Update uses
// optimistic locking (resource_version), so a concurrent state change between Get and
// Update causes a version conflict rather than a silent overwrite.
func (s *PrivatePublicIPsServer) Update(ctx context.Context,
	request *privatev1.PublicIPsUpdateRequest) (response *privatev1.PublicIPsUpdateResponse, err error) {
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.PublicIPsGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.PublicIPsGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingPublicIP := getResponse.GetObject()
	mask := request.GetUpdateMask()

	if updateIncludesField(mask, "spec.pool") {
		if err = validateImmutableFieldsPublicIP(request.GetObject(), existingPublicIP); err != nil {
			return
		}
	}

	if updateIncludesField(mask, "status.state") {
		newState := request.GetObject().GetStatus().GetState()
		existingState := existingPublicIP.GetStatus().GetState()
		if newState != existingState {
			if err = validatePublicIPStateTransition(existingState, newState); err != nil {
				return
			}
		}
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

// Delete rejects deletion of ATTACHED PublicIPs and restores pool capacity on success.
// All operations share the gRPC interceptor's transaction for atomicity.
func (s *PrivatePublicIPsServer) Delete(ctx context.Context,
	request *privatev1.PublicIPsDeleteRequest) (response *privatev1.PublicIPsDeleteResponse, err error) {
	// Fetch the existing PublicIP to check state and get pool ID:
	id := request.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.PublicIPsGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.PublicIPsGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingPublicIP := getResponse.GetObject()

	state := existingPublicIP.GetStatus().GetState()
	if state != privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED {
		err = grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"cannot delete PublicIP in state %s: must be in ALLOCATED state", state)
		return
	}

	// Delete the PublicIP:
	err = s.generic.Delete(ctx, request, &response)
	if err != nil {
		return
	}

	// Update pool capacity: increment available, decrement allocated:
	poolID := existingPublicIP.GetSpec().GetPool()
	if poolID != "" {
		err = s.updatePoolCapacity(ctx, poolID, -1)
		if err != nil {
			return
		}
	}

	return
}

func (s *PrivatePublicIPsServer) Signal(ctx context.Context,
	request *privatev1.PublicIPsSignalRequest) (response *privatev1.PublicIPsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validatePublicIP validates the PublicIP object before creation.
func (s *PrivatePublicIPsServer) validatePublicIP(ctx context.Context,
	publicIP *privatev1.PublicIP) error {
	if publicIP == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP is mandatory")
	}
	spec := publicIP.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP spec is mandatory")
	}
	if spec.GetPool() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.pool' is required")
	}
	return nil
}

// validatePoolReference validates that the referenced PublicIPPool exists, is in READY state,
// and has available capacity.
func (s *PrivatePublicIPsServer) validatePoolReference(ctx context.Context, poolID string) error {
	getResponse, err := s.publicIPPoolDao.Get().
		SetId(poolID).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"pool '%s' does not exist", poolID)
		}
		s.logger.ErrorContext(ctx, "Failed to query PublicIPPool",
			slog.String("pool_id", poolID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to validate pool")
	}

	pool := getResponse.GetObject()

	// Check pool is in READY state:
	if pool.GetStatus().GetState() != privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"pool '%s' is not in READY state (current state: %s)",
			poolID, pool.GetStatus().GetState().String())
	}

	// Best-effort check for user-facing error quality. The real serialization
	// point is updatePoolCapacity, which uses the pool's resource_version for
	// optimistic locking to prevent concurrent over-allocation.
	if pool.GetStatus().GetAvailable() <= 0 {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"pool '%s' has no available capacity", poolID)
	}

	return nil
}

// updatePoolCapacity atomically updates the PublicIPPool capacity counters.
// A positive delta means allocation (increment allocated, decrement available).
// A negative delta means release (decrement allocated, increment available).
func (s *PrivatePublicIPsServer) updatePoolCapacity(ctx context.Context, poolID string, delta int64) error {
	poolResponse, err := s.publicIPPoolDao.Get().
		SetId(poolID).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get pool for capacity update",
			slog.String("pool_id", poolID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to update pool capacity")
	}

	pool := poolResponse.GetObject()
	newAllocated := pool.GetStatus().GetAllocated() + delta
	newAvailable := pool.GetStatus().GetAvailable() - delta
	if newAvailable < 0 {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"pool '%s' has no available capacity", poolID)
	}
	if newAllocated < 0 {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"pool '%s' capacity inconsistency: allocated would become %d (available: %d)",
			poolID, newAllocated, newAvailable)
	}
	pool.GetStatus().SetAllocated(newAllocated)
	pool.GetStatus().SetAvailable(newAvailable)

	_, err = s.publicIPPoolDao.Update().
		SetObject(pool).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update pool capacity",
			slog.String("pool_id", poolID),
			slog.Any("error", err))
		var conflictErr *dao.ErrConflict
		if errors.As(err, &conflictErr) {
			return grpcstatus.Errorf(grpccodes.Aborted, "%s", conflictErr.Error())
		}
		return grpcstatus.Errorf(grpccodes.Internal, "failed to update pool capacity")
	}

	return nil
}

// validateImmutableFieldsPublicIP validates that immutable fields have not been changed on Update.
func validateImmutableFieldsPublicIP(newPublicIP, existingPublicIP *privatev1.PublicIP) error {
	if existingPublicIP == nil {
		return nil // Create operation, no immutability checks
	}

	newPool := newPublicIP.GetSpec().GetPool()
	existingPool := existingPublicIP.GetSpec().GetPool()

	if newPool != existingPool {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.pool' is immutable and cannot be changed from '%s' to '%s'",
			existingPool, newPool)
	}

	return nil
}

// validatePublicIPStateTransition checks whether a state transition is valid according to the
// PublicIP lifecycle state machine.
func validatePublicIPStateTransition(from, to privatev1.PublicIPState) error {
	allowed, exists := validPublicIPTransitions[from]
	if !exists || !slices.Contains(allowed, to) {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"invalid state transition from %s to %s", from.String(), to.String())
	}

	return nil
}
