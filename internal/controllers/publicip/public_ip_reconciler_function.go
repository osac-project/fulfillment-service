/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	osacv1alpha1 "github.com/osac-project/osac-operator/api/v1alpha1"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/annotations"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// objectPrefix is the prefix that will be used in the `generateName` field of the resources created in the hub.
const objectPrefix = "publicip-"

// FunctionBuilder contains the data and logic needed to build a function that reconciles public IPs.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger              *slog.Logger
	hubCache            controllers.HubCache
	publicIPsClient     privatev1.PublicIPsClient
	publicIPPoolsClient privatev1.PublicIPPoolsClient
	maskCalculator      *masks.Calculator
}

type task struct {
	r            *function
	publicIP     *privatev1.PublicIP
	hubId        string
	hubNamespace string
	hubClient    clnt.Client
}

// NewFunction creates a new builder that can then be used to create a new public IP reconciler function.
func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC client connection. This is mandatory.
func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

// SetHubCache sets the cache of hubs. This is mandatory.
func (b *FunctionBuilder) SetHubCache(value controllers.HubCache) *FunctionBuilder {
	b.hubCache = value
	return b
}

// Build uses the information stored in the builder to create a new public IP reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.PublicIP], err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("client is mandatory")
		return
	}
	if b.hubCache == nil {
		err = errors.New("hub cache is mandatory")
		return
	}

	// Create and populate the object:
	object := &function{
		logger:              b.logger,
		publicIPsClient:     privatev1.NewPublicIPsClient(b.connection),
		publicIPPoolsClient: privatev1.NewPublicIPPoolsClient(b.connection),
		hubCache:            b.hubCache,
		maskCalculator:      masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, publicIP *privatev1.PublicIP) error {
	oldPublicIP := proto.Clone(publicIP).(*privatev1.PublicIP)
	t := task{
		r:        r,
		publicIP: publicIP,
	}
	var err error
	if publicIP.HasMetadata() && publicIP.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	updateMask := r.maskCalculator.Calculate(oldPublicIP, publicIP)
	if len(updateMask.GetPaths()) == 0 {
		return nil
	}

	_, err = r.publicIPsClient.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
		Object:     publicIP,
		UpdateMask: updateMask,
	}.Build())

	return err
}

func (t *task) update(ctx context.Context) error {
	// Add the finalizer and return immediately if it was added. This ensures the finalizer is persisted before any
	// other work is done, reducing the chance of the object being deleted before the finalizer is saved.
	if t.addFinalizer() {
		return nil
	}

	// Set the default values:
	t.setDefaults()

	// Validate that exactly one tenant is assigned:
	if err := t.validateTenant(); err != nil {
		return err
	}

	// Select the hub from the parent pool:
	if err := t.selectHub(ctx); err != nil {
		return err
	}

	// Save the selected hub in the status of the public IP:
	t.publicIP.GetStatus().SetHub(t.hubId)

	// Get the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return err
	}

	// Prepare the changes to the spec:
	spec := t.buildSpec()

	// Create or update the Kubernetes object:
	if object == nil {
		newObject := &osacv1alpha1.PublicIP{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    t.hubNamespace,
				GenerateName: objectPrefix,
				Labels: map[string]string{
					labels.PublicIPUuid: t.publicIP.GetId(),
				},
				Annotations: map[string]string{
					annotations.Tenant: t.publicIP.GetMetadata().GetTenants()[0],
				},
			},
			Spec: spec,
		}
		err = t.hubClient.Create(ctx, newObject)
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Created public IP",
			slog.String("namespace", newObject.GetNamespace()),
			slog.String("name", newObject.GetName()),
		)
	} else {
		update := object.DeepCopy()
		update.Spec = spec
		err = t.hubClient.Patch(ctx, update, clnt.MergeFrom(object))
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Updated public IP",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return err
}

func (t *task) setDefaults() {
	if !t.publicIP.HasStatus() {
		t.publicIP.SetStatus(&privatev1.PublicIPStatus{})
	}
	if t.publicIP.GetStatus().GetState() == privatev1.PublicIPState_PUBLIC_IP_STATE_UNSPECIFIED {
		t.publicIP.GetStatus().SetState(privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)
	}
}

func (t *task) validateTenant() error {
	if !t.publicIP.HasMetadata() || len(t.publicIP.GetMetadata().GetTenants()) != 1 {
		return errors.New("public IP must have exactly one tenant assigned")
	}
	return nil
}

func (t *task) delete(ctx context.Context) (err error) {
	// Do nothing if we don't know the hub yet:
	t.hubId = t.publicIP.GetStatus().GetHub()
	if t.hubId == "" {
		// No hub assigned, nothing to clean up on K8s side.
		t.removeFinalizer()
		return nil
	}
	err = t.getHub(ctx)
	if err != nil {
		return
	}

	// Check if the K8S object still exists:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return
	}
	if object == nil {
		// K8s object is fully gone (all K8s finalizers processed).
		// Safe to remove our DB finalizer and allow archiving.
		t.r.logger.DebugContext(
			ctx,
			"Public IP doesn't exist",
			slog.String("id", t.publicIP.GetId()),
		)
		t.removeFinalizer()
		return
	}

	// Initiate K8s deletion if not already in progress:
	if object.GetDeletionTimestamp() == nil {
		err = t.hubClient.Delete(ctx, object)
		if err != nil {
			return
		}
		t.r.logger.DebugContext(
			ctx,
			"Deleted public IP",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		t.r.logger.DebugContext(
			ctx,
			"Public IP is still being deleted, waiting for K8s finalizers",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	// Don't remove finalizer: K8s object still exists with finalizers being processed.
	return
}

// selectHub derives the hub from the parent PublicIPPool instead of random selection.
// If the public IP already has a hub assigned, it uses that directly.
// If the pool has no hub yet (not reconciled), the public IP is skipped and retried next cycle.
func (t *task) selectHub(ctx context.Context) error {
	t.hubId = t.publicIP.GetStatus().GetHub()
	if t.hubId == "" {
		// Look up the parent pool to derive the hub:
		poolResponse, err := t.r.publicIPPoolsClient.Get(ctx, privatev1.PublicIPPoolsGetRequest_builder{
			Id: t.publicIP.GetSpec().GetPool(),
		}.Build())
		if err != nil {
			return err
		}
		poolHub := poolResponse.GetObject().GetStatus().GetHub()
		if poolHub == "" {
			return fmt.Errorf(
				"pool %s has no hub assigned yet, skipping",
				t.publicIP.GetSpec().GetPool(),
			)
		}
		t.hubId = poolHub
	}
	t.r.logger.DebugContext(
		ctx,
		"Selected hub",
		slog.String("id", t.hubId),
	)
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	return nil
}

func (t *task) getHub(ctx context.Context) error {
	t.hubId = t.publicIP.GetStatus().GetHub()
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	return nil
}

func (t *task) getKubeObject(ctx context.Context) (result *osacv1alpha1.PublicIP, err error) {
	list := &osacv1alpha1.PublicIPList{}
	err = t.hubClient.List(
		ctx, list,
		clnt.InNamespace(t.hubNamespace),
		clnt.MatchingLabels{
			labels.PublicIPUuid: t.publicIP.GetId(),
		},
	)
	if err != nil {
		return
	}
	items := list.Items
	count := len(items)
	if count > 1 {
		err = fmt.Errorf(
			"expected at most one public IP with identifier '%s' but found %d",
			t.publicIP.GetId(), count,
		)
		return
	}
	if count > 0 {
		result = &items[0]
	}
	return
}

// addFinalizer adds the controller finalizer if it is not already present. Returns true if the finalizer was added,
// false if it was already present.
func (t *task) addFinalizer() bool {
	if !t.publicIP.HasMetadata() {
		t.publicIP.SetMetadata(&privatev1.Metadata{})
	}
	list := t.publicIP.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.publicIP.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	if !t.publicIP.HasMetadata() {
		return
	}
	list := t.publicIP.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.publicIP.GetMetadata().SetFinalizers(list)
	}
}

// buildSpec constructs the spec map for the Kubernetes PublicIP object based on the
// public IP from the database. Only spec fields are pushed to the CRD; status fields
// (address, state) originate from the operator side and flow K8s -> fulfillment-service.
func (t *task) buildSpec() osacv1alpha1.PublicIPSpec {
	spec := osacv1alpha1.PublicIPSpec{
		Pool: t.publicIP.GetSpec().GetPool(),
	}
	return spec
}
