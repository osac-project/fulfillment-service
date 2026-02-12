/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/innabox/fulfillment-service/internal/api/private/v1"
	"github.com/innabox/fulfillment-service/internal/controllers"
	"github.com/innabox/fulfillment-service/internal/controllers/finalizers"
	"github.com/innabox/fulfillment-service/internal/kubernetes/gvks"
	"github.com/innabox/fulfillment-service/internal/kubernetes/labels"
	"github.com/innabox/fulfillment-service/internal/masks"
)

// objectPrefix is the prefix that will be used in the `generateName` field of the resources created in the hub.
const objectPrefix = "host-"

// FunctionBuilder contains the data and logic needed to build a function that reconciles hosts.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger          *slog.Logger
	hubCache        controllers.HubCache
	hostsClient     privatev1.HostsClient
	hostPoolsClient privatev1.HostPoolsClient
	hubsClient      privatev1.HubsClient
	maskCalculator  *masks.Calculator
}

type task struct {
	r            *function
	host         *privatev1.Host
	hubId        string
	hubNamespace string
	hubClient    clnt.Client
}

// NewFunction creates a new builder that can then be used to create a new host reconciler function.
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

// Build uses the information stored in the builder to create a new host reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.Host], err error) {
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
		hostsClient:     privatev1.NewHostsClient(b.connection),
		hostPoolsClient: privatev1.NewHostPoolsClient(b.connection),
		hubsClient:      privatev1.NewHubsClient(b.connection),
		hubCache:        b.hubCache,
		maskCalculator:  masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, host *privatev1.Host) error {
	oldHost := proto.Clone(host).(*privatev1.Host)
	t := task{
		r:    r,
		host: host,
	}
	var err error
	if host.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	// Calculate which fields the reconciler actually modified and use a field mask
	// to update only those fields. This prevents overwriting concurrent user changes.
	updateMask := r.maskCalculator.Calculate(oldHost, host)

	// Only send an update if there are actual changes
	if len(updateMask.GetPaths()) > 0 {
		_, err = r.hostsClient.Update(ctx, privatev1.HostsUpdateRequest_builder{
			Object:     host,
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

	// TODO: add host state field so we can see if the update is needed
	//if t.host.GetStatus().GetState() != privatev1.HostState_HOST_STATE_PROGRESSING {
	//	return nil
	//}

	// Select the hub:
	err := t.selectHub(ctx)
	if err != nil {
		return err
	}

	// Get the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return err
	}

	// Prepare the changes to the spec:
	spec := map[string]any{
		"powerState": t.host.GetSpec().GetPowerState().String(),
	}

	// Create or update the Kubernetes object:
	if object == nil {
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(gvks.Host)
		object.SetNamespace(t.hubNamespace)
		object.SetGenerateName(objectPrefix)
		object.SetLabels(map[string]string{
			labels.HostUuid: t.host.GetId(),
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
			"Created host",
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
			"Updated host",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return err
}

func (t *task) setDefaults() {
	if !t.host.HasStatus() {
		t.host.SetStatus(&privatev1.HostStatus{})
	}
	if t.host.GetStatus().GetPowerState() == privatev1.HostPowerState_HOST_POWER_STATE_UNSPECIFIED {
		t.host.GetStatus().SetPowerState(privatev1.HostPowerState_HOST_POWER_STATE_OFF)
	}
	// Host doesn't have conditions in the current protobuf definition
}

// Host doesn't have conditions in the current protobuf definition

func (t *task) delete(ctx context.Context) (err error) {
	// Remember to remove the finalizer if there was no error:
	defer func() {
		if err == nil {
			t.removeFinalizer()
		}
	}()

	// For hosts, we need to select a hub to find and delete the object
	err = t.selectHub(ctx)
	if err != nil {
		return
	}

	// Delete the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return
	}
	if object == nil {
		t.r.logger.DebugContext(
			ctx,
			"Host doesn't exist",
			slog.String("id", t.host.GetId()),
		)
		return
	}
	err = t.hubClient.Delete(ctx, object)
	if err != nil {
		return
	}
	t.r.logger.DebugContext(
		ctx,
		"Deleted host",
		slog.String("namespace", object.GetNamespace()),
		slog.String("name", object.GetName()),
	)

	return
}

func (t *task) selectHub(ctx context.Context) error {
	// Use the Hub from the parent host pool
	hostPoolId := t.host.GetStatus().GetHostPool()
	if hostPoolId == "" {
		return errors.New("host is not associated with a host pool")
	}
	response, err := t.r.hostPoolsClient.Get(ctx, privatev1.HostPoolsGetRequest_builder{Id: hostPoolId}.Build())
	if err != nil {
		return err
	}
	t.hubId = response.GetObject().GetStatus().GetHub()
	if t.hubId == "" {
		return errors.New("parent host pool is not associated with a hub")
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
	list.SetGroupVersionKind(gvks.HostList)
	err = t.hubClient.List(
		ctx, list,
		clnt.InNamespace(t.hubNamespace),
		clnt.MatchingLabels{
			labels.HostUuid: t.host.GetId(),
		},
	)
	if err != nil {
		return
	}
	items := list.Items
	count := len(items)
	if count > 1 {
		err = fmt.Errorf(
			"expected at most one host with identifier '%s' but found %d",
			t.host.GetId(), count,
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
	list := t.host.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.host.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	list := t.host.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.host.GetMetadata().SetFinalizers(list)
	}
}
