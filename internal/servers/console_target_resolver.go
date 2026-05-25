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
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	osacv1alpha1 "github.com/osac-project/osac-operator/api/v1alpha1"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/console"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

// ComputeInstanceLookup provides compute instance data for console resolution.
// Implementations are pure readers that consume a tx-bound context.
type ComputeInstanceLookup interface {
	GetForConsole(ctx context.Context, id string) (*ConsoleComputeInstanceInfo, error)
}

// ConsoleComputeInstanceInfo is the subset of compute instance state needed by
// the resolver: running status and hub assignment.
type ConsoleComputeInstanceInfo struct {
	State privatev1.ComputeInstanceState
	HubID string
}

// HubLookup provides hub cluster access for console resolution.
// Implementations are pure readers that consume a tx-bound context.
type HubLookup interface {
	GetKubeconfig(ctx context.Context, hubID string) (kubeconfig []byte, namespace string, err error)
}

// ConsoleTargetResolverBuilder builds a ConsoleTargetResolver.
type ConsoleTargetResolverBuilder struct {
	logger           *slog.Logger
	ciLookup         ComputeInstanceLookup
	hubLookup        HubLookup
	hubClientFactory HubClientFactory
	txManager        database.TxManager
}

// ConsoleTargetResolver resolves a resource type and ID to a console.Target. The gRPC console
// server uses it during session creation to resolve the target before sealing it into the
// encrypted ticket.
type ConsoleTargetResolver struct {
	logger           *slog.Logger
	ciLookup         ComputeInstanceLookup
	hubLookup        HubLookup
	hubClientFactory HubClientFactory
	txManager        database.TxManager
}

// NewConsoleTargetResolver creates a new builder for the console target resolver.
func NewConsoleTargetResolver() *ConsoleTargetResolverBuilder {
	return &ConsoleTargetResolverBuilder{}
}

func (b *ConsoleTargetResolverBuilder) SetLogger(value *slog.Logger) *ConsoleTargetResolverBuilder {
	b.logger = value
	return b
}

func (b *ConsoleTargetResolverBuilder) SetComputeInstanceLookup(value ComputeInstanceLookup) *ConsoleTargetResolverBuilder {
	b.ciLookup = value
	return b
}

func (b *ConsoleTargetResolverBuilder) SetHubLookup(value HubLookup) *ConsoleTargetResolverBuilder {
	b.hubLookup = value
	return b
}

func (b *ConsoleTargetResolverBuilder) SetHubClientFactory(value HubClientFactory) *ConsoleTargetResolverBuilder {
	b.hubClientFactory = value
	return b
}

func (b *ConsoleTargetResolverBuilder) SetTxManager(value database.TxManager) *ConsoleTargetResolverBuilder {
	b.txManager = value
	return b
}

func (b *ConsoleTargetResolverBuilder) Build() (*ConsoleTargetResolver, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.ciLookup == nil {
		return nil, errors.New("compute instance lookup is mandatory")
	}
	if b.hubLookup == nil {
		return nil, errors.New("hub lookup is mandatory")
	}
	if b.hubClientFactory == nil {
		return nil, errors.New("hub client factory is mandatory")
	}
	if b.txManager == nil {
		return nil, errors.New("transaction manager is mandatory")
	}
	return &ConsoleTargetResolver{
		logger:           b.logger,
		ciLookup:         b.ciLookup,
		hubLookup:        b.hubLookup,
		hubClientFactory: b.hubClientFactory,
		txManager:        b.txManager,
	}, nil
}

// ResolveComputeInstance resolves a compute instance ID and console type to a fully populated
// console.Target including the pre-computed backend URI and token. It manages its own transaction
// if no transaction is present in the context (e.g. streaming RPCs without the tx interceptor),
// or reuses an existing one (unary RPCs like Create).
func (r *ConsoleTargetResolver) ResolveComputeInstance(ctx context.Context, resourceID, consoleType string) (result *console.Target, err error) {
	// Check if there is already a transaction in the context (e.g. from the unary tx interceptor).
	// Only create a new one if needed.
	_, txErr := database.TxFromContext(ctx)
	if txErr != nil {
		tx, beginErr := r.txManager.Begin(ctx)
		if beginErr != nil {
			return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", beginErr)
		}
		defer func() {
			if endErr := r.txManager.End(ctx, tx); endErr != nil && err == nil {
				err = status.Errorf(codes.Internal, "transaction cleanup failed: %v", endErr)
			}
		}()
		ctx = database.TxIntoContext(ctx, tx)
	}

	// Look up the compute instance to check its state and get the hub assignment.
	ciInfo, err := r.ciLookup.GetForConsole(ctx, resourceID)
	if err != nil {
		// Preserve the original gRPC status code if available (e.g., Internal for DB
		// errors, Unavailable for transient failures) so clients can retry appropriately.
		if st, ok := status.FromError(err); ok {
			return nil, status.Errorf(st.Code(), "failed to get compute instance %q: %v", resourceID, st.Message())
		}
		return nil, status.Errorf(codes.Internal, "failed to get compute instance %q: %v", resourceID, err)
	}

	// Verify running state.
	if ciInfo.State != privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING {
		return nil, status.Errorf(codes.FailedPrecondition,
			"compute instance %q is not running (state: %s)", resourceID, ciInfo.State.String())
	}

	// Verify hub assignment.
	if ciInfo.HubID == "" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"compute instance %q has no hub assigned", resourceID)
	}

	// Query the hub cluster for the ComputeInstance CR and get the kubeconfig.
	namespace, crName, kubeconfig, err := r.getComputeInstanceFromHub(ctx, ciInfo.HubID, resourceID)
	if err != nil {
		return nil, err
	}

	// Build the pre-computed backend target from the kubeconfig.
	backendURI, backendToken, err := console.BuildBackendTarget(kubeconfig, namespace, crName, consoleType)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build backend target: %v", err)
	}

	return &console.Target{
		ResourceType: "compute_instance",
		ResourceID:   resourceID,
		HubID:        ciInfo.HubID,
		Namespace:    namespace,
		CRName:       crName,
		ConsoleType:  consoleType,
		BackendURI:   backendURI,
		BackendToken: backendToken,
	}, nil
}

// getComputeInstanceFromHub queries the hub cluster for the ComputeInstance CR matching the given
// instance ID, and returns its namespace, name, and the raw kubeconfig bytes.
func (r *ConsoleTargetResolver) getComputeInstanceFromHub(ctx context.Context, hubID, instanceID string) (namespace, crName string, kubeconfig []byte, err error) {
	// Get the hub's kubeconfig and namespace.
	kubeconfig, hubNamespace, err := r.hubLookup.GetKubeconfig(ctx, hubID)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			err = status.Errorf(st.Code(), "failed to get hub %q: %v", hubID, st.Message())
		} else {
			err = status.Errorf(codes.Internal, "failed to get hub %q: %v", hubID, err)
		}
		return
	}
	if hubNamespace == "" {
		err = status.Errorf(codes.Internal, "hub %q returned empty namespace", hubID)
		return
	}

	// Create a Kubernetes client for the hub cluster.
	hubClient, err := r.hubClientFactory(kubeconfig)
	if err != nil {
		err = status.Errorf(codes.Internal, "failed to create client for hub %q: %v", hubID, err)
		return
	}

	// Query for the ComputeInstance CR by UUID label.
	list := &osacv1alpha1.ComputeInstanceList{}
	err = hubClient.List(
		ctx, list,
		clnt.InNamespace(hubNamespace),
		clnt.MatchingLabels{
			labels.ComputeInstanceUuid: instanceID,
		},
	)
	if err != nil {
		err = status.Errorf(codes.Internal, "failed to list compute instances on hub %q: %v", hubID, err)
		return
	}

	items := list.Items
	if len(items) == 0 {
		r.logger.WarnContext(ctx, "Running compute instance not found on hub",
			slog.String("instance_id", instanceID),
			slog.String("hub_id", hubID),
		)
		err = status.Errorf(codes.FailedPrecondition,
			"compute instance %q not found on hub %q; it may still be provisioning", instanceID, hubID)
		return
	}
	if len(items) > 1 {
		err = status.Errorf(codes.Internal,
			"expected one compute instance with ID %q on hub %q but found %d", instanceID, hubID, len(items))
		return
	}

	obj := items[0]
	if obj.Status.Phase != osacv1alpha1.ComputeInstancePhaseRunning {
		phase := string(obj.Status.Phase)
		r.logger.WarnContext(ctx, "Compute instance is not running on hub",
			slog.String("instance_id", instanceID),
			slog.String("hub_id", hubID),
			slog.String("cr_name", obj.GetName()),
			slog.String("phase", phase),
		)
		msg := fmt.Sprintf(
			"compute instance %q is not running on hub %q (phase: %s)",
			instanceID, hubID, phase)
		if obj.Status.Phase == osacv1alpha1.ComputeInstancePhaseStarting {
			msg += "; it may still be provisioning"
		}
		err = status.Errorf(codes.FailedPrecondition, "%s", msg)
		return
	}
	return obj.GetNamespace(), obj.GetName(), kubeconfig, nil
}

// privateServerCILookup wraps the private ComputeInstancesServer to implement ComputeInstanceLookup.
// It is a pure reader -- the caller provides a tx-bound context.
type privateServerCILookup struct {
	ciServer privatev1.ComputeInstancesServer
}

// NewPrivateServerCILookup creates a ComputeInstanceLookup backed by the private ComputeInstances server.
func NewPrivateServerCILookup(ciServer privatev1.ComputeInstancesServer) ComputeInstanceLookup {
	return &privateServerCILookup{ciServer: ciServer}
}

func (l *privateServerCILookup) GetForConsole(ctx context.Context, id string) (*ConsoleComputeInstanceInfo, error) {
	resp, err := l.ciServer.Get(ctx, privatev1.ComputeInstancesGetRequest_builder{
		Id: id,
	}.Build())
	if err != nil {
		return nil, err
	}
	ci := resp.GetObject()
	ciStatus := ci.GetStatus()

	return &ConsoleComputeInstanceInfo{
		State: ciStatus.GetState(),
		HubID: ciStatus.GetHub(),
	}, nil
}

// privateServerHubLookup wraps the private HubsServer to implement HubLookup.
// It is a pure reader -- the caller provides a tx-bound context.
type privateServerHubLookup struct {
	hubServer privatev1.HubsServer
}

// NewPrivateServerHubLookup creates a HubLookup backed by the private Hubs server.
func NewPrivateServerHubLookup(hubServer privatev1.HubsServer) HubLookup {
	return &privateServerHubLookup{hubServer: hubServer}
}

func (l *privateServerHubLookup) GetKubeconfig(ctx context.Context, hubID string) (kubeconfig []byte, namespace string, err error) {
	hubResp, err := l.hubServer.Get(ctx, privatev1.HubsGetRequest_builder{
		Id: hubID,
	}.Build())
	if err != nil {
		return nil, "", err
	}
	hub := hubResp.GetObject()
	return hub.GetSpec().GetKubeconfig(), hub.GetSpec().GetNamespace(), nil
}
