/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

// LeaderElectorBuilder contains the data and logic needed to create a LeaderElector.
type LeaderElectorBuilder struct {
	logger        *slog.Logger
	client        *grpc.ClientConn
	leaseId       string
	leaseHolder   string
	leaseDuration time.Duration
	callback      func(ctx context.Context)
}

// LeaderElector coordinates leader election among multiple instances of a component using the lease API. Use the
// SetCallback method to provide the function that performs the leader's work. That function will be called when this
// instance becomes the leader, and it receives a context that will be cancelled when leadership is lost.
type LeaderElector struct {
	logger        *slog.Logger
	client        privatev1.LeasesClient
	leaseId       string
	leaseHolder   string
	leaseDuration time.Duration
	renewInterval time.Duration
	retryInterval time.Duration
	callback      func(ctx context.Context)
	lease         *privatev1.Lease
}

// NewLeaderElector creates a builder that can then be used to configure and create a leader elector.
func NewLeaderElector() *LeaderElectorBuilder {
	return &LeaderElectorBuilder{
		leaseDuration: leaderElectorDefaultLeaseDuration,
	}
}

// SetLogger sets the logger. This is mandatory.
func (b *LeaderElectorBuilder) SetLogger(value *slog.Logger) *LeaderElectorBuilder {
	b.logger = value
	return b
}

// SetClient sets the gRPC client connection used to talk to the Leases service. This is mandatory.
func (b *LeaderElectorBuilder) SetClient(value *grpc.ClientConn) *LeaderElectorBuilder {
	b.client = value
	return b
}

// SetLeaseId sets the identifier of lease. All instances that should participate in the same leader election must use
// the same identifier. This is mandatory.
func (b *LeaderElectorBuilder) SetLeaseId(value string) *LeaderElectorBuilder {
	b.leaseId = value
	return b
}

// SetLeaseHolder sets a string that uniquely identifies this instance as a holder of the lease. This is typically a
// hostname or a combination of hostname and process identifier. This is mandatory.
func (b *LeaderElectorBuilder) SetLeaseHolder(value string) *LeaderElectorBuilder {
	b.leaseHolder = value
	return b
}

// SetLeaseDuration sets how long the lease is valid after its last renewal. If the holder fails to renew within
// this duration, the lease is considered expired. Note that the lease may have been already created by another
// instance with a different duration. In that case the duration of the existing lease will be adopted ignoring
// this value. The default is 15 seconds.
func (b *LeaderElectorBuilder) SetLeaseDuration(value time.Duration) *LeaderElectorBuilder {
	b.leaseDuration = value
	return b
}

// SetCallback sets the function that will be called when this instance becomes the leader. The context passed
// to the function will be cancelled when leadership is lost. The function should block until the context is
// cancelled or its work is complete. This is mandatory.
func (b *LeaderElectorBuilder) SetCallback(value func(ctx context.Context)) *LeaderElectorBuilder {
	b.callback = value
	return b
}

// Build uses the configuration stored in the builder to create a new LeaderElector.
func (b *LeaderElectorBuilder) Build() (result *LeaderElector, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.client == nil {
		err = errors.New("client is mandatory")
		return
	}
	if b.leaseId == "" {
		err = errors.New("identifier is mandatory")
		return
	}
	if b.leaseHolder == "" {
		err = errors.New("holder is mandatory")
		return
	}
	if b.callback == nil {
		err = errors.New("callback is mandatory")
		return
	}
	if b.leaseDuration <= 0 {
		err = fmt.Errorf("lease duration must be positive, but it is %s", b.leaseDuration)
		return
	}

	// Add some information to the logger:
	logger := b.logger.With(
		slog.String("lease", b.leaseId),
		slog.String("holder", b.leaseHolder),
	)

	// Create and populate the object:
	result = &LeaderElector{
		logger:        logger,
		client:        privatev1.NewLeasesClient(b.client),
		leaseId:       b.leaseId,
		leaseHolder:   b.leaseHolder,
		leaseDuration: b.leaseDuration,
		callback:      b.callback,
	}

	return
}

// Run starts the leader election loop. It blocks until the context is cancelled. When this instance becomes the
// leader, the callback is invoked with a context that will be cancelled when leadership is lost.
func (e *LeaderElector) Run(ctx context.Context) error {
	e.calculateIntervals(ctx)
	e.logger.DebugContext(
		ctx,
		"Starting leader election",
		slog.Duration("duration", e.leaseDuration),
		slog.Duration("renew", e.renewInterval),
		slog.Duration("retry", e.retryInterval),
	)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := e.ensureLease(ctx)
		if err != nil {
			e.logger.ErrorContext(
				ctx,
				"Failed to get or create lease",
				slog.Any("error", err),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(e.retryInterval):
			}
			continue
		}

		if e.isLeader() {
			e.lead(ctx)
			continue
		}

		if e.isExpired() {
			err := e.acquireLease(ctx)
			if err != nil {
				e.logger.ErrorContext(
					ctx,
					"Failed to acquire lease, will retry",
					slog.Any("error", err),
				)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(e.retryInterval):
				}
				continue
			}
			e.lead(ctx)
			continue
		}

		e.logger.DebugContext(
			ctx,
			"Waiting for leader to relinquish lease",
			slog.String("leader", e.lease.GetSpec().GetHolder()),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.retryInterval):
		}
	}
}

// lead runs the leading loop: invokes the callback and periodically renews the lease. When renewal fails or the
// context is cancelled, the leader context is cancelled which signals the callback to stop.
func (e *LeaderElector) lead(ctx context.Context) {
	e.logger.DebugContext(ctx, "Became leader")

	leadCtx, leadCancel := context.WithCancel(ctx)
	defer leadCancel()

	leadDone := make(chan struct{})
	go func() {
		defer close(leadDone)
		e.callback(leadCtx)
	}()

	ticker := time.NewTicker(e.renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			leadCancel()
			<-leadDone
			return
		case <-leadDone:
			return
		case <-ticker.C:
			err := e.renewLease(ctx)
			if err != nil {
				e.logger.ErrorContext(
					ctx,
					"Failed to renew lease, losing leadership",
					slog.Any("error", err),
				)
				leadCancel()
				<-leadDone
				return
			}
		}
	}
}

// calculateIntervals calculates the renew and retry intervals based on the lease duration. If the lease has
// already been fetched from the database, its duration is used; otherwise the configured default is used.
func (e *LeaderElector) calculateIntervals(ctx context.Context) {
	var leaseDuration time.Duration
	if e.lease != nil {
		leaseDuration = e.lease.GetSpec().GetDuration().AsDuration()
	} else {
		leaseDuration = e.leaseDuration
	}
	if leaseDuration < leaderElectorMinLeaseDuration {
		e.logger.WarnContext(
			ctx,
			"Lease duration is too small, using minimum",
			slog.Duration("configured", leaseDuration),
			slog.Duration("minimum", leaderElectorMinLeaseDuration),
		)
		leaseDuration = leaderElectorMinLeaseDuration
	}
	e.renewInterval = leaseDuration * 80 / 100
	e.retryInterval = leaseDuration / 10
	e.logger.DebugContext(
		ctx,
		"Calculated intervals",
		slog.Duration("lease", leaseDuration),
		slog.Duration("renew", e.renewInterval),
		slog.Duration("retry", e.retryInterval),
	)
}

// ensureLease retrieves the existing lease by identifier, or creates a new one if none exists. If creation
// fails because another instance created the lease concurrently, it retries the get once. The result is stored
// in the lease field of the elector.
func (e *LeaderElector) ensureLease(ctx context.Context) (err error) {
	// Make sure to recalculate the intervals after successfully fetching or creating the lease:
	defer func() {
		if err != nil {
			return
		}
		e.calculateIntervals(ctx)
	}()

	// Try to get an existing lease, and if it doesn't exist try to create it. If creation fails because
	// another instance created it concurrently, retry the get once.
	for attempt := range 2 {
		getResponse, getErr := e.client.Get(ctx, privatev1.LeasesGetRequest_builder{
			Id: e.leaseId,
		}.Build())
		if getErr == nil {
			e.lease = getResponse.GetObject()
			e.logger.DebugContext(ctx, "Got existing lease")
			return nil
		}
		if grpcstatus.Code(getErr) != grpccodes.NotFound {
			return fmt.Errorf("failed to get lease '%s': %w", e.leaseId, getErr)
		}

		if attempt > 0 {
			return fmt.Errorf("failed to get lease '%s' after creation conflict", e.leaseId)
		}

		now := timestamppb.Now()
		createResponse, createErr := e.client.Create(ctx, privatev1.LeasesCreateRequest_builder{
			Object: privatev1.Lease_builder{
				Id: e.leaseId,
				Spec: privatev1.LeaseSpec_builder{
					Holder:           e.leaseHolder,
					Duration:         durationpb.New(e.leaseDuration),
					AcquireTimestamp: now,
					RenewTimestamp:   now,
				}.Build(),
			}.Build(),
		}.Build())
		if createErr == nil {
			e.lease = createResponse.GetObject()
			e.logger.DebugContext(
				ctx,
				"Created lease",
				slog.Any("details", e.lease),
			)
			return nil
		}
		if grpcstatus.Code(createErr) != grpccodes.AlreadyExists {
			return fmt.Errorf("failed to create lease '%s': %w", e.leaseId, createErr)
		}
		e.logger.DebugContext(ctx, "Lease was created concurrently, retrying get")
	}

	return fmt.Errorf("failed to ensure lease '%s'", e.leaseId)
}

// isLeader returns true if the lease is currently held by this instance.
func (e *LeaderElector) isLeader() bool {
	return e.lease.GetSpec().GetHolder() == e.leaseHolder
}

// isExpired returns true if the lease has expired based on its renew time and duration.
func (e *LeaderElector) isExpired() bool {
	spec := e.lease.GetSpec()
	renew := spec.GetRenewTimestamp().AsTime()
	duration := spec.GetDuration().AsDuration()
	return time.Now().After(renew.Add(duration))
}

// acquireLease takes over an expired lease by updating the holder identity and timestamps. It uses optimistic locking
// via the metadata version to ensure that if another instance concurrently modifies the lease, only one of them
// succeeds. The lease field is updated with the server's response on success.
func (e *LeaderElector) acquireLease(ctx context.Context) error {
	now := timestamppb.Now()
	response, err := e.client.Update(ctx, privatev1.LeasesUpdateRequest_builder{
		Object: privatev1.Lease_builder{
			Id: e.leaseId,
			Metadata: privatev1.Metadata_builder{
				Version: e.lease.GetMetadata().GetVersion(),
			}.Build(),
			Spec: privatev1.LeaseSpec_builder{
				Holder:           e.leaseHolder,
				AcquireTimestamp: now,
				RenewTimestamp:   now,
			}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{
				"spec.holder",
				"spec.acquire_timestamp",
				"spec.renew_timestamp",
			},
		},
		Lock: true,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to acquire lease '%s': %w", e.leaseId, err)
	}
	e.lease = response.GetObject()
	e.logger.DebugContext(
		ctx,
		"Acquired lease",
		slog.Int("version", int(e.lease.GetMetadata().GetVersion())),
	)
	return nil
}

// renewLease extends the lease by updating the renew time. It uses optimistic locking via the metadata version to
// prevent concurrent modifications. The lease field is updated with the server's response on success.
func (e *LeaderElector) renewLease(ctx context.Context) error {
	response, err := e.client.Update(ctx, privatev1.LeasesUpdateRequest_builder{
		Object: privatev1.Lease_builder{
			Id: e.leaseId,
			Metadata: privatev1.Metadata_builder{
				Version: e.lease.GetMetadata().GetVersion(),
			}.Build(),
			Spec: privatev1.LeaseSpec_builder{
				RenewTimestamp: timestamppb.Now(),
			}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{
				"spec.renew_timestamp",
			},
		},
		Lock: true,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to renew lease '%s': %w", e.leaseId, err)
	}
	e.lease = response.GetObject()
	e.logger.DebugContext(
		ctx,
		"Renewed lease",
		slog.Int("version", int(e.lease.GetMetadata().GetVersion())),
	)
	return nil
}

// Default and minimum values for the leader elector configuration:
const (
	leaderElectorDefaultLeaseDuration = 15 * time.Second
	leaderElectorMinLeaseDuration     = 1 * time.Second
)
