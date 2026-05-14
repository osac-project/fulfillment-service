/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

//go:generate mockgen -source=../../api/osac/private/v1/projects_service_grpc.pb.go -destination=projects_client_mock.go -package=project ProjectsClient
//go:generate mockgen -source=../../api/osac/private/v1/hubs_service_grpc.pb.go -destination=hubs_client_mock.go -package=project HubsClient

package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/annotations"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// FunctionBuilder contains the data needed to build instances of the reconciler function.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

// NewFunction creates a builder that can be used to configure and create reconciler functions.
func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

// SetLogger sets the logger that the reconciler will use to write log messages.
func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC connection that the reconciler will use to communicate with the API server.
func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

// SetHubCache sets the cache of hubs. This is mandatory.
func (b *FunctionBuilder) SetHubCache(value controllers.HubCache) *FunctionBuilder {
	b.hubCache = value
	return b
}

// Build uses the data stored in the builder to create and configure a new reconciler function.
func (b *FunctionBuilder) Build() (result *function, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("connection is mandatory")
		return
	}
	if b.hubCache == nil {
		err = errors.New("hub cache is mandatory")
		return
	}

	result = &function{
		logger:         b.logger,
		projectsClient: privatev1.NewProjectsClient(b.connection),
		hubsClient:     privatev1.NewHubsClient(b.connection),
		hubCache:       b.hubCache,
		maskCalculator: masks.NewCalculator().Build(),
	}
	return
}

// function is the implementation of the reconciler function.
type function struct {
	logger         *slog.Logger
	projectsClient privatev1.ProjectsClient
	hubsClient     privatev1.HubsClient
	hubCache       controllers.HubCache
	maskCalculator *masks.Calculator
}

// Run executes the reconciliation logic for the given project.
func (r *function) Run(ctx context.Context, project *privatev1.Project) error {
	oldProject := proto.Clone(project).(*privatev1.Project)

	task := &task{
		r:       r,
		project: project,
	}

	var err error
	if project.HasMetadata() && project.GetMetadata().HasDeletionTimestamp() {
		err = task.delete(ctx)
	} else {
		err = task.update(ctx)
	}
	if err != nil {
		return err
	}

	updateMask := r.maskCalculator.Calculate(oldProject, project)

	if len(updateMask.GetPaths()) > 0 {
		_, err = r.projectsClient.Update(ctx, privatev1.ProjectsUpdateRequest_builder{
			Object:     project,
			UpdateMask: updateMask,
		}.Build())
	}

	return err
}

// task contains the data needed to reconcile a single project.
type task struct {
	r       *function
	project *privatev1.Project
}

// update performs the reconciliation logic for creating or updating a project.
func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	t.setDefaults()

	state := t.project.GetStatus().GetState()

	// Skip reconciliation for terminal states to prevent infinite loops.
	if state == privatev1.ProjectState_PROJECT_STATE_FAILED ||
		state == privatev1.ProjectState_PROJECT_STATE_DELETE_FAILED {
		return nil
	}

	// For active projects, no re-validation needed (parent is immutable).
	if state == privatev1.ProjectState_PROJECT_STATE_ACTIVE {
		return nil
	}

	// Project is PENDING or UNSPECIFIED, perform validation
	return t.validateAndActivate(ctx)
}

// validateAndActivate validates the project hierarchy and transitions to ACTIVE or FAILED state.
func (t *task) validateAndActivate(ctx context.Context) error {
	t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_PENDING)

	// Validate parent project if specified
	if t.project.GetSpec().HasParent() {
		parentID := t.project.GetSpec().GetParent()

		// Prevent self-reference
		if parentID == t.project.GetId() {
			t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_FAILED)
			t.project.GetStatus().SetMessage("Project cannot be its own parent")
			return nil
		}

		// Fetch parent project
		parentResp, err := t.r.projectsClient.Get(ctx, privatev1.ProjectsGetRequest_builder{
			Id: parentID,
		}.Build())
		if err != nil {
			if status.Code(err) == codes.NotFound {
				t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_FAILED)
				t.project.GetStatus().SetMessage(fmt.Sprintf("Parent project not found: %s", parentID))
				return nil
			}
			// Transient error - return it to retry later
			return fmt.Errorf("failed to fetch parent project: %w", err)
		}

		parentProject := parentResp.GetObject()

		// Validate parent is in ACTIVE state
		parentState := parentProject.GetStatus().GetState()
		if parentState != privatev1.ProjectState_PROJECT_STATE_ACTIVE {
			t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_FAILED)
			t.project.GetStatus().SetMessage(fmt.Sprintf("Parent project %s is not in ACTIVE state (current state: %s)", parentProject.GetId(), parentState))
			return nil
		}

		// Check for circular dependencies by traversing up the hierarchy
		if err := t.checkCircularDependency(ctx, parentProject); err != nil {
			t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_FAILED)
			t.project.GetStatus().SetMessage(err.Error())
			return nil
		}
	}

	// Sync OpenShift Projects to all hubs
	if err := t.syncOpenShiftProjects(ctx); err != nil {
		// Transient error - return it to retry later
		return fmt.Errorf("failed to sync OpenShift projects: %w", err)
	}

	// Check if sync set state to FAILED (e.g., name too long)
	if t.project.GetStatus().GetState() == privatev1.ProjectState_PROJECT_STATE_FAILED {
		return nil
	}

	//TODO: Create Keycloak Authorization Resource

	// All validations passed
	t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_ACTIVE)
	t.project.GetStatus().ClearMessage()

	t.r.logger.DebugContext(ctx, "Project activated",
		slog.String("project_id", t.project.GetId()),
		slog.String("project_name", t.project.GetMetadata().GetName()),
	)

	return nil
}

// checkCircularDependency traverses the parent hierarchy to detect circular dependencies.
// Accepts the already-fetched immediate parent to avoid redundant RPC calls.
func (t *task) checkCircularDependency(ctx context.Context, parent *privatev1.Project) error {
	visited := make(map[string]bool)
	visited[t.project.GetId()] = true
	visited[parent.GetId()] = true

	currentParent := parent
	maxDepth := 100 // Prevent infinite loops from unexpected state

	for depth := 0; depth < maxDepth; depth++ {
		// If parent has no parent, we've reached the top of the hierarchy
		if !currentParent.GetSpec().HasParent() {
			return nil
		}

		nextParentID := currentParent.GetSpec().GetParent()

		// Check if we've seen this parent before (circular dependency)
		if visited[nextParentID] {
			return fmt.Errorf("Circular dependency detected in project hierarchy")
		}
		visited[nextParentID] = true

		// Fetch the next parent project
		parentResp, err := t.r.projectsClient.Get(ctx, privatev1.ProjectsGetRequest_builder{
			Id: nextParentID,
		}.Build())
		if err != nil {
			if status.Code(err) == codes.NotFound {
				// Parent not found, but we already validated it exists above, so this is transient
				return fmt.Errorf("parent project disappeared during validation: %w", err)
			}
			return fmt.Errorf("failed to fetch parent project during circular dependency check: %w", err)
		}

		currentParent = parentResp.GetObject()
	}

	// If we hit max depth, treat it as a circular dependency
	return fmt.Errorf("Project hierarchy exceeds maximum depth of %d", maxDepth)
}

// syncOpenShiftProjects creates or updates OpenShift Projects (namespaces) across all managed hubs.
func (t *task) syncOpenShiftProjects(ctx context.Context) error {
	tenant := t.getTenant()
	namespaceName := t.buildNamespaceName(tenant)

	// Validate namespace name length
	if !t.validateNamespaceNameLength(namespaceName) {
		return nil
	}

	// List all hubs
	hubsResp, err := t.r.hubsClient.List(ctx, privatev1.HubsListRequest_builder{}.Build())
	if err != nil {
		t.updateCondition(
			privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC,
			privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
			"HubListFailed",
			fmt.Sprintf("Failed to list hubs: %v", err),
		)
		return fmt.Errorf("failed to list hubs: %w", err)
	}

	if len(hubsResp.Items) == 0 {
		t.updateCondition(
			privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC,
			privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
			"NoHubs",
			"No hubs available to sync OpenShift Projects",
		)
		return nil
	}

	// Track hub sync failures
	var failedHubs []string
	totalHubs := len(hubsResp.Items)

	// For each hub, create or update the OpenShift Project
	for _, hub := range hubsResp.Items {
		hubEntry, err := t.r.hubCache.Get(ctx, hub.GetId())
		if err != nil {
			t.r.logger.WarnContext(ctx, "Failed to get hub client",
				slog.String("hub_id", hub.GetId()),
				slog.String("error", err.Error()),
			)
			failedHubs = append(failedHubs, hub.GetId())
			continue
		}

		// Create or update the namespace
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}

		_, err = controllerutil.CreateOrUpdate(ctx, hubEntry.Client, namespace, func() error {
			namespace.Labels, namespace.Annotations = t.buildNamespaceMetadata(tenant)
			return nil
		})

		if err != nil {
			t.r.logger.WarnContext(ctx, "Failed to create or update namespace",
				slog.String("hub_id", hub.GetId()),
				slog.String("namespace", namespaceName),
				slog.String("error", err.Error()),
			)
			failedHubs = append(failedHubs, hub.GetId())
			continue
		}

		t.r.logger.DebugContext(ctx, "Created or updated OpenShift Project",
			slog.String("hub_id", hub.GetId()),
			slog.String("namespace", namespaceName),
		)
	}

	// Update condition based on results
	t.updateHubSyncCondition(totalHubs, failedHubs)
	return nil
}

// getTenant returns the first tenant from the project metadata, or empty string if none.
func (t *task) getTenant() string {
	if t.project.HasMetadata() {
		return t.project.GetMetadata().GetTenant()
	}
	return ""
}

// buildNamespaceName generates the OpenShift Project (namespace) name for the project.
// Format: osac-tenant-<tenant>-project-<name>
func (t *task) buildNamespaceName(tenant string) string {
	projectName := t.project.GetMetadata().GetName()
	return fmt.Sprintf("osac-tenant-%s-project-%s", tenant, projectName)
}

// validateNamespaceNameLength validates that the namespace name doesn't exceed Kubernetes 63-character limit.
// Returns true if valid, false if invalid (and sets FAILED state with appropriate conditions).
func (t *task) validateNamespaceNameLength(namespaceName string) bool {
	if len(namespaceName) <= 63 {
		return true
	}

	t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_FAILED)
	t.project.GetStatus().SetMessage(fmt.Sprintf(
		"Generated OpenShift Project name '%s' exceeds 63 character limit (length: %d). "+
			"Please use shorter tenant and project names.",
		namespaceName, len(namespaceName)))
	t.updateCondition(
		privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC,
		privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
		"NameTooLong",
		fmt.Sprintf("OpenShift Project name '%s' exceeds 63 character limit", namespaceName),
	)
	return false
}

// buildNamespaceMetadata builds the labels and annotations for an OpenShift Project namespace.
func (t *task) buildNamespaceMetadata(tenant string) (map[string]string, map[string]string) {
	// Build labels
	namespaceLabels := map[string]string{
		labels.ProjectId:   t.project.GetId(),
		labels.ProjectName: t.project.GetMetadata().GetName(),
	}

	// Build annotations
	namespaceAnnotations := map[string]string{}
	if tenant != "" {
		namespaceAnnotations[annotations.Tenant] = tenant
	}
	if t.project.GetSpec().HasParent() {
		namespaceAnnotations[annotations.ParentProject] = t.project.GetSpec().GetParent()
	}

	return namespaceLabels, namespaceAnnotations
}

// delete performs the deletion cleanup for a project.
func (t *task) delete(ctx context.Context) error {
	t.removeFinalizer()
	return nil
}

// setDefaults sets default values for the project.
func (t *task) setDefaults() {
	if !t.project.HasStatus() {
		t.project.SetStatus(&privatev1.ProjectStatus{})
	}
	if t.project.GetStatus().GetState() == privatev1.ProjectState_PROJECT_STATE_UNSPECIFIED {
		t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_PENDING)
	}
	// Initialize default conditions
	for value := range privatev1.ProjectConditionType_name {
		if value != 0 {
			t.setConditionDefaults(privatev1.ProjectConditionType(value))
		}
	}
}

// setConditionDefaults ensures a condition exists with a default state if not already present.
func (t *task) setConditionDefaults(conditionType privatev1.ProjectConditionType) {
	exists := false
	for _, current := range t.project.GetStatus().GetConditions() {
		if current.GetType() == conditionType {
			exists = true
			break
		}
	}
	if !exists {
		conditions := t.project.GetStatus().GetConditions()
		conditions = append(conditions, privatev1.ProjectCondition_builder{
			Type:   conditionType,
			Status: privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
		}.Build())
		t.project.GetStatus().SetConditions(conditions)
	}
}

// addFinalizer adds the controller finalizer to the project if not already present.
// Returns true if the finalizer was added (indicating the update should be saved immediately).
func (t *task) addFinalizer() bool {
	if !t.project.HasMetadata() {
		t.project.SetMetadata(&privatev1.Metadata{})
	}
	list := t.project.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.project.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

// removeFinalizer removes the controller finalizer from the project.
func (t *task) removeFinalizer() {
	if !t.project.HasMetadata() {
		return
	}
	list := t.project.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.project.GetMetadata().SetFinalizers(list)
	}
}

// updateHubSyncCondition updates the HUB_SYNC condition based on hub sync results.
func (t *task) updateHubSyncCondition(totalHubs int, failedHubs []string) {
	successCount := totalHubs - len(failedHubs)

	switch {
	case len(failedHubs) == 0:
		// All hubs synced successfully
		t.updateCondition(
			privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC,
			privatev1.ConditionStatus_CONDITION_STATUS_TRUE,
			"HubsSynced",
			fmt.Sprintf("OpenShift Projects created on all %d hubs", totalHubs),
		)
	case successCount > 0:
		// Partial success
		t.updateCondition(
			privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC,
			privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
			"PartialSync",
			fmt.Sprintf("OpenShift Projects created on %d/%d hubs (failed: %s)",
				successCount, totalHubs, strings.Join(failedHubs, ", ")),
		)
	default:
		// All failed
		t.updateCondition(
			privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC,
			privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
			"SyncFailed",
			fmt.Sprintf("Failed to sync to all %d hubs", totalHubs),
		)
	}
}

// updateCondition updates or creates a condition with the specified type, status, reason, and message.
func (t *task) updateCondition(conditionType privatev1.ProjectConditionType, status privatev1.ConditionStatus,
	reason string, message string) {
	conditions := t.project.GetStatus().GetConditions()
	updated := false
	for i, condition := range conditions {
		if condition.GetType() == conditionType {
			conditions[i] = privatev1.ProjectCondition_builder{
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
		conditions = append(conditions, privatev1.ProjectCondition_builder{
			Type:    conditionType,
			Status:  status,
			Reason:  &reason,
			Message: &message,
		}.Build())
	}
	t.project.GetStatus().SetConditions(conditions)
}
