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

package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/idp"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// FunctionBuilder contains the data needed to build instances of the reconciler function.
type FunctionBuilder struct {
	logger          *slog.Logger
	connection      *grpc.ClientConn
	resourceManager *idp.ResourceManager
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

// SetResourceManager sets the resource manager that the reconciler will use to manage authorization resources.
func (b *FunctionBuilder) SetResourceManager(value *idp.ResourceManager) *FunctionBuilder {
	b.resourceManager = value
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
	if b.resourceManager == nil {
		err = errors.New("resource manager is mandatory")
		return
	}

	result = &function{
		logger:          b.logger,
		projectsClient:  privatev1.NewProjectsClient(b.connection),
		resourceManager: b.resourceManager,
		maskCalculator:  masks.NewCalculator().Build(),
	}
	return
}

// function is the implementation of the reconciler function.
type function struct {
	logger          *slog.Logger
	projectsClient  privatev1.ProjectsClient
	resourceManager *idp.ResourceManager
	maskCalculator  *masks.Calculator
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

	//TODO: Sync project to Openshift Cluster(s)

	// Create Keycloak Authorization Resource
	if err := t.createKeycloakAuthorizationResource(ctx); err != nil {
		// Transient error - return it to retry later
		return fmt.Errorf("failed to create Keycloak authorization resource: %w", err)
	}

	// Check if Keycloak resource creation set the state to FAILED
	if t.project.GetStatus().GetState() == privatev1.ProjectState_PROJECT_STATE_FAILED {
		return nil
	}

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

// delete performs the deletion cleanup for a project.
func (t *task) delete(ctx context.Context) error {
	t.removeFinalizer()
	return nil
}

// createKeycloakAuthorizationResource creates a Keycloak Authorization Resource for the project.
// This resource enables fine-grained permission management for project operations.
func (t *task) createKeycloakAuthorizationResource(ctx context.Context) error {
	// Create the resource via ResourceManager
	resourceID, err := t.r.resourceManager.CreateProjectAuthorizationResource(
		ctx,
		t.project.GetId(),
		t.project.GetMetadata().GetTenant(),
		t.project.GetMetadata().GetName(),
		[]string{
			idp.ScopeViewProject,
			idp.ScopeManageProject,
		},
	)
	if err != nil {
		// Update condition with failure
		t.updateCondition(
			privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_KEYCLOAK_SYNC,
			privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
			"CreationFailed",
			fmt.Sprintf("Failed to create Keycloak authorization resource: %v", err),
		)
		// Return error to trigger retry (could be transient)
		return fmt.Errorf("failed to create Keycloak authorization resource: %w", err)
	}

	// Update condition with success
	t.updateCondition(
		privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_KEYCLOAK_SYNC,
		privatev1.ConditionStatus_CONDITION_STATUS_TRUE,
		"Created",
		fmt.Sprintf("Keycloak authorization resource created: %s", resourceID),
	)

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
