/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/innabox/fulfillment-service/internal/api/private/v1"
	sharedv1 "github.com/innabox/fulfillment-service/internal/api/shared/v1"
	"github.com/innabox/fulfillment-service/internal/controllers"
	"github.com/innabox/fulfillment-service/internal/controllers/finalizers"
	"github.com/innabox/fulfillment-service/internal/kubernetes/annotations"
	"github.com/innabox/fulfillment-service/internal/kubernetes/gvks"
	"github.com/innabox/fulfillment-service/internal/kubernetes/labels"
	"github.com/innabox/fulfillment-service/internal/masks"
	"github.com/innabox/fulfillment-service/internal/utils"
)

// objectPrefix is the prefix that will be used in the `generateName` field of the resources created in the hub.
const objectPrefix = "vm-"

// FunctionBuilder contains the data and logic needed to build a function that reconciles compute instances.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger                 *slog.Logger
	hubCache               controllers.HubCache
	computeInstancesClient privatev1.ComputeInstancesClient
	hubsClient             privatev1.HubsClient
	maskCalculator         *masks.Calculator
}

type task struct {
	r               *function
	computeInstance *privatev1.ComputeInstance
	hubId           string
	hubNamespace    string
	hubClient       clnt.Client
}

// NewFunction creates a new builder that can then be used to create a new compute instance reconciler function.
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

// Build uses the information stored in the builder to create a new compute instance reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.ComputeInstance], err error) {
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
		logger:                 b.logger,
		computeInstancesClient: privatev1.NewComputeInstancesClient(b.connection),
		hubsClient:             privatev1.NewHubsClient(b.connection),
		hubCache:               b.hubCache,
		maskCalculator:         masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, computeInstance *privatev1.ComputeInstance) error {
	oldComputeInstance := proto.Clone(computeInstance).(*privatev1.ComputeInstance)
	t := task{
		r:               r,
		computeInstance: computeInstance,
	}
	var err error
	if computeInstance.HasMetadata() && computeInstance.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	// Calculate which fields the reconciler actually modified and use a field mask
	// to update only those fields. This prevents overwriting concurrent user changes.
	updateMask := r.maskCalculator.Calculate(oldComputeInstance, computeInstance)

	// Only send an update if there are actual changes
	_, err = r.computeInstancesClient.Update(ctx, privatev1.ComputeInstancesUpdateRequest_builder{
		Object:     computeInstance,
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

	// Select the hub:
	if err := t.selectHub(ctx); err != nil {
		return err
	}

	// Save the selected hub in the private data of the compute instance:
	t.computeInstance.GetStatus().SetHub(t.hubId)

	// Get the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return err
	}

	// Prepare the changes to the spec:
	spec, err := t.buildSpec()
	if err != nil {
		return err
	}

	// Create or update the Kubernetes object:
	if object == nil {
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(gvks.ComputeInstance)
		object.SetNamespace(t.hubNamespace)
		object.SetGenerateName(objectPrefix)
		object.SetLabels(map[string]string{
			labels.ComputeInstanceUuid: t.computeInstance.GetId(),
		})
		object.SetAnnotations(map[string]string{
			annotations.ComputeInstanceTenant: t.computeInstance.GetMetadata().GetTenants()[0],
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
			"Created compute instance",
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
			"Updated compute instance",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return err
}

func (t *task) setDefaults() {
	if !t.computeInstance.HasStatus() {
		t.computeInstance.SetStatus(&privatev1.ComputeInstanceStatus{})
	}
	if t.computeInstance.GetStatus().GetState() == privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_UNSPECIFIED {
		t.computeInstance.GetStatus().SetState(privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_PROGRESSING)
	}
	for value := range privatev1.ComputeInstanceConditionType_name {
		if value != 0 {
			t.setConditionDefaults(privatev1.ComputeInstanceConditionType(value))
		}
	}
}

func (t *task) setConditionDefaults(value privatev1.ComputeInstanceConditionType) {
	// Check if condition already exists
	exists := false
	for _, current := range t.computeInstance.GetStatus().GetConditions() {
		if current.GetType() == value {
			exists = true
			break
		}
	}
	// Only set default if condition doesn't exist
	if !exists {
		t.updateCondition(value, sharedv1.ConditionStatus_CONDITION_STATUS_FALSE, "", "")
	}
}

func (t *task) validateTenant() error {
	if !t.computeInstance.HasMetadata() || len(t.computeInstance.GetMetadata().GetTenants()) != 1 {
		message := "Compute instance must have exactly one tenant assigned"
		err := errors.New(message)
		t.updateCondition(
			privatev1.ComputeInstanceConditionType_COMPUTE_INSTANCE_CONDITION_TYPE_PROGRESSING,
			sharedv1.ConditionStatus_CONDITION_STATUS_FALSE,
			"InvalidTenant",
			message,
		)
		return err
	}
	return nil
}

func (t *task) delete(ctx context.Context) (err error) {
	// Do nothing if we don't know the hub yet:
	t.hubId = t.computeInstance.GetStatus().GetHub()
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
			"Compute instance doesn't exist",
			slog.String("id", t.computeInstance.GetId()),
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
			"Deleted compute instance",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		t.r.logger.DebugContext(
			ctx,
			"Compute instance is still being deleted, waiting for K8s finalizers",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	// Don't remove finalizer — K8s object still exists with finalizers being processed.
	return
}

func (t *task) selectHub(ctx context.Context) error {
	t.hubId = t.computeInstance.GetStatus().GetHub()
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
	t.hubId = t.computeInstance.GetStatus().GetHub()
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
	list.SetGroupVersionKind(gvks.ComputeInstanceList)
	err = t.hubClient.List(
		ctx, list,
		clnt.InNamespace(t.hubNamespace),
		clnt.MatchingLabels{
			labels.ComputeInstanceUuid: t.computeInstance.GetId(),
		},
	)
	if err != nil {
		return
	}
	items := list.Items
	count := len(items)
	if count > 1 {
		err = fmt.Errorf(
			"expected at most one compute instance with identifier '%s' but found %d",
			t.computeInstance.GetId(), count,
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
	if !t.computeInstance.HasMetadata() {
		t.computeInstance.SetMetadata(&privatev1.Metadata{})
	}
	list := t.computeInstance.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.computeInstance.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	if !t.computeInstance.HasMetadata() {
		return
	}
	list := t.computeInstance.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.computeInstance.GetMetadata().SetFinalizers(list)
	}
}

// updateCondition updates or creates a condition with the specified type, status, reason, and message.
func (t *task) updateCondition(conditionType privatev1.ComputeInstanceConditionType, status sharedv1.ConditionStatus,
	reason string, message string) {
	conditions := t.computeInstance.GetStatus().GetConditions()
	updated := false
	for i, condition := range conditions {
		if condition.GetType() == conditionType {
			conditions[i] = privatev1.ComputeInstanceCondition_builder{
				Type:    conditionType,
				Status:  status,
				Reason:  &reason,
				Message: &message,
			}.Build()
			updated = true
			break
		}
	}
	if !updated {
		conditions = append(conditions, privatev1.ComputeInstanceCondition_builder{
			Type:    conditionType,
			Status:  status,
			Reason:  &reason,
			Message: &message,
		}.Build())
	}
	t.computeInstance.GetStatus().SetConditions(conditions)
}

// buildSpec constructs the spec map for the Kubernetes ComputeInstance object based on the
// compute instance from the database.
func (t *task) buildSpec() (map[string]any, error) {
	templateParameters, err := utils.ConvertTemplateParametersToJSON(t.computeInstance.GetSpec().GetTemplateParameters())
	if err != nil {
		return nil, err
	}
	spec := map[string]any{
		"templateID":         t.computeInstance.GetSpec().GetTemplate(),
		"templateParameters": templateParameters,
	}

	// Add restartRequestedAt if present:
	if t.computeInstance.GetSpec().HasRestartRequestedAt() {
		spec["restartRequestedAt"] = t.computeInstance.GetSpec().GetRestartRequestedAt().AsTime().Format(time.RFC3339)
	}

	return spec, nil
}
