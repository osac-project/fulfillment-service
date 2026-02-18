/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package hostpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	sharedv1 "github.com/osac-project/fulfillment-service/internal/api/shared/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// objectPrefix is the prefix that will be used in the `generateName` field of the resources created in the hub.
const objectPrefix = "hostpool-"

// FunctionBuilder contains the data and logic needed to build a function that reconciles host pools.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger          *slog.Logger
	hubCache        controllers.HubCache
	hostPoolsClient privatev1.HostPoolsClient
	hubsClient      privatev1.HubsClient
	maskCalculator  *masks.Calculator
}

type task struct {
	r            *function
	hostPool     *privatev1.HostPool
	hubId        string
	hubNamespace string
	hubClient    clnt.Client
}

// NewFunction creates a new builder that can then be used to create a new host pool reconciler function.
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

// Build uses the information stored in the builder to create a new host pool reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.HostPool], err error) {
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
		logger:          b.logger,
		hostPoolsClient: privatev1.NewHostPoolsClient(b.connection),
		hubsClient:      privatev1.NewHubsClient(b.connection),
		hubCache:        b.hubCache,
		maskCalculator:  masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, hostPool *privatev1.HostPool) error {
	oldHostPool := proto.Clone(hostPool).(*privatev1.HostPool)
	t := task{
		r:        r,
		hostPool: hostPool,
	}
	var err error
	if hostPool.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	// Calculate which fields the reconciler actually modified and use a field mask
	// to update only those fields. This prevents overwriting concurrent user changes.
	updateMask := r.maskCalculator.Calculate(oldHostPool, hostPool)

	// Only send an update if there are actual changes
	if len(updateMask.GetPaths()) > 0 {
		_, err = r.hostPoolsClient.Update(ctx, privatev1.HostPoolsUpdateRequest_builder{
			Object:     hostPool,
			UpdateMask: updateMask,
		}.Build())
	}
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

	// Do nothing if the host pool isn't progressing:
	if t.hostPool.GetStatus().GetState() != privatev1.HostPoolState_HOST_POOL_STATE_PROGRESSING {
		return nil
	}

	// Select the hub:
	err := t.selectHub(ctx)
	if err != nil {
		return err
	}

	// Save the selected hub in the private data of the host pool:
	t.hostPool.GetStatus().SetHub(t.hubId)

	// Get the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return err
	}

	// Prepare the changes to the spec:
	hostSets := t.prepareHostSetRequests()
	spec := map[string]any{
		"hostSets": hostSets,
	}

	// Create or update the Kubernetes object:
	if object == nil {
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(gvks.HostPool)
		object.SetNamespace(t.hubNamespace)
		object.SetGenerateName(objectPrefix)
		object.SetLabels(map[string]string{
			labels.HostPoolUuid: t.hostPool.GetId(),
		})
		err = unstructured.SetNestedField(object.Object, spec, "spec")
		if err != nil {
			return err
		}
		err = t.hubClient.Create(ctx, object)
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Created host pool",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		update := object.DeepCopy()
		err = unstructured.SetNestedField(update.Object, spec, "spec")
		if err != nil {
			return err
		}
		err = t.hubClient.Patch(ctx, update, clnt.MergeFrom(object))
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Updated host pool",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return err
}

func (t *task) setDefaults() {
	if !t.hostPool.HasStatus() {
		t.hostPool.SetStatus(&privatev1.HostPoolStatus{})
	}
	if t.hostPool.GetStatus().GetState() == privatev1.HostPoolState_HOST_POOL_STATE_UNSPECIFIED {
		t.hostPool.GetStatus().SetState(privatev1.HostPoolState_HOST_POOL_STATE_PROGRESSING)
	}
	for value := range privatev1.HostPoolConditionType_name {
		if value != 0 {
			t.setConditionDefaults(privatev1.HostPoolConditionType(value))
		}
	}
}

func (t *task) setConditionDefaults(value privatev1.HostPoolConditionType) {
	exists := false
	for _, current := range t.hostPool.GetStatus().GetConditions() {
		if current.GetType() == value {
			exists = true
			break
		}
	}
	if !exists {
		conditions := t.hostPool.GetStatus().GetConditions()
		conditions = append(conditions, privatev1.HostPoolCondition_builder{
			Type:   value,
			Status: sharedv1.ConditionStatus_CONDITION_STATUS_FALSE,
		}.Build())
		t.hostPool.GetStatus().SetConditions(conditions)
	}
}

func (t *task) prepareHostSetRequests() any {
	var hostSetRequests []any
	for _, hostSet := range t.hostPool.GetSpec().GetHostSets() {
		hostSetRequest := t.prepareHostSetRequest(hostSet)
		hostSetRequests = append(hostSetRequests, hostSetRequest)
	}
	return hostSetRequests
}

func (t *task) prepareHostSetRequest(hostSet *privatev1.HostPoolHostSet) any {
	return map[string]any{
		"hostClass": hostSet.GetHostClass(),
		"size":      int64(hostSet.GetSize()),
	}
}

func (t *task) delete(ctx context.Context) (err error) {
	// Remember to remove the finalizer if there was no error:
	defer func() {
		if err == nil {
			t.removeFinalizer()
		}
	}()

	// For host pools, we need to select a hub to find and delete the object
	err = t.selectHub(ctx)
	if err != nil {
		return err
	}

	// Delete the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return
	}
	if object == nil {
		t.r.logger.DebugContext(
			ctx,
			"Host pool doesn't exist",
			slog.String("id", t.hostPool.GetId()),
		)
		return
	}
	err = t.hubClient.Delete(ctx, object)
	if err != nil {
		return
	}
	t.r.logger.DebugContext(
		ctx,
		"Deleted host pool",
		slog.String("namespace", object.GetNamespace()),
		slog.String("name", object.GetName()),
	)

	return
}

func (t *task) selectHub(ctx context.Context) error {
	t.hubId = t.hostPool.GetStatus().GetHub()
	if t.hubId == "" {
		response, err := t.r.hubsClient.List(ctx, privatev1.HubsListRequest_builder{}.Build())
		if err != nil {
			return err
		}
		if len(response.Items) == 0 {
			return errors.New("there are no hubs")
		}
		t.hubId = response.Items[rand.IntN(len(response.Items))].GetId()
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
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	return nil
}

func (t *task) getKubeObject(ctx context.Context) (result *unstructured.Unstructured, err error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvks.HostPoolList)
	err = t.hubClient.List(
		ctx, list,
		clnt.InNamespace(t.hubNamespace),
		clnt.MatchingLabels{
			labels.HostPoolUuid: t.hostPool.GetId(),
		},
	)
	if err != nil {
		return
	}
	items := list.Items
	count := len(items)
	if count > 1 {
		err = fmt.Errorf(
			"expected at most one host pool with identifier '%s' but found %d",
			t.hostPool.GetId(), count,
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
	list := t.hostPool.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.hostPool.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	list := t.hostPool.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.hostPool.GetMetadata().SetFinalizers(list)
	}
}
