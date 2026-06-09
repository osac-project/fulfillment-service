/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/idp"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// FunctionBuilder contains the data needed to build instances of the reconciler function.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	idpManager *idp.OrganizationManager
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

// SetIdpManager sets the IDP manager that the reconciler will use to manage organizations in the identity provider.
func (b *FunctionBuilder) SetIdpManager(value *idp.OrganizationManager) *FunctionBuilder {
	b.idpManager = value
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
	if b.idpManager == nil {
		err = errors.New("IDP manager is mandatory")
		return
	}

	result = &function{
		logger:              b.logger,
		organizationsClient: privatev1.NewOrganizationsClient(b.connection),
		idpManager:          b.idpManager,
		maskCalculator:      masks.NewCalculator().Build(),
	}
	return
}

// function is the implementation of the reconciler function.
type function struct {
	logger              *slog.Logger
	organizationsClient privatev1.OrganizationsClient
	idpManager          *idp.OrganizationManager
	maskCalculator      *masks.Calculator
}

// Run executes the reconciliation logic for the given organization.
func (r *function) Run(ctx context.Context, organization *privatev1.Organization) error {
	oldOrg := proto.Clone(organization).(*privatev1.Organization)

	task := &task{
		r:            r,
		organization: organization,
	}

	var err error
	if organization.HasMetadata() && organization.GetMetadata().HasDeletionTimestamp() {
		err = task.delete(ctx)
	} else {
		err = task.update(ctx)
	}
	if err != nil {
		return err
	}

	updateMask := r.maskCalculator.Calculate(oldOrg, organization)

	if len(updateMask.GetPaths()) > 0 {
		_, err = r.organizationsClient.Update(ctx, privatev1.OrganizationsUpdateRequest_builder{
			Object:     organization,
			UpdateMask: updateMask,
		}.Build())
	}

	return err
}

// task contains the data needed to reconcile a single organization.
type task struct {
	r            *function
	organization *privatev1.Organization
}

// update performs the reconciliation logic for creating or updating an organization.
func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	t.setDefaults()

	if err := t.validateTenant(); err != nil {
		return err
	}

	state := t.organization.GetStatus().GetState()

	// Skip reconciliation only for terminal failure state.
	// This prevents infinite retry loops when IDP operations fail.
	if state == privatev1.OrganizationState_ORGANIZATION_STATE_FAILED {
		return nil
	}

	// For synced organizations, update the IDP with any spec changes (e.g. domains).
	if state == privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED {
		return t.updateIDP(ctx)
	}

	// Organization is PENDING or UNSPECIFIED, perform initial sync to IDP
	return t.syncToIDP(ctx)
}

// syncToIDP synchronizes the organization to the identity provider.
func (t *task) syncToIDP(ctx context.Context) error {
	t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING)

	orgName := t.organization.GetMetadata().GetName()
	config := &idp.OrganizationConfig{
		Name:    orgName,
		Enabled: new(!t.isBuiltin()),
		Domains: t.organization.GetSpec().GetDomains(),
	}

	credentials, err := t.r.idpManager.CreateOrganization(ctx, config)
	if err != nil {
		t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_FAILED)
		t.organization.GetStatus().SetMessage(fmt.Sprintf("Organization creation in IDP failed: %v", err))
		return nil
	}

	t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED)
	t.organization.GetStatus().SetIdpOrganizationName(config.Name)
	t.organization.GetStatus().SetBreakGlassUserId(credentials.UserID)

	breakGlassCredentials := privatev1.BreakGlassCredentials_builder{
		Username: credentials.Username,
		Password: credentials.Password,
	}.Build()
	t.organization.GetStatus().SetBreakGlassCredentials(breakGlassCredentials)

	t.r.logger.DebugContext(ctx, "Organization synced to IDP",
		slog.String("organization_id", t.organization.GetId()),
		slog.String("organization_name", orgName),
	)

	return nil
}

// updateIDP updates the organization in the identity provider with the current spec values.
func (t *task) updateIDP(ctx context.Context) error {
	orgName := t.organization.GetStatus().GetIdpOrganizationName()
	if orgName == "" {
		t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_FAILED)
		t.organization.GetStatus().SetMessage("Organization name is empty")
		t.r.logger.ErrorContext(
			ctx,
			"Organization name is empty",
			slog.String("tenant", t.organization.GetMetadata().GetName()),
		)
		return nil
	}
	domains := t.organization.GetSpec().GetDomains()
	err := t.r.idpManager.UpdateOrganization(ctx, orgName, domains)
	if err != nil {
		t.r.logger.ErrorContext(ctx, "Failed to update organization domains in IDP",
			slog.String("organization_id", t.organization.GetId()),
			slog.Any("error", err),
		)
		return err
	}
	return nil
}

// setDefaults sets default values for the organization.
func (t *task) setDefaults() {
	if !t.organization.HasStatus() {
		t.organization.SetStatus(&privatev1.OrganizationStatus{})
	}
	if t.organization.GetStatus().GetState() == privatev1.OrganizationState_ORGANIZATION_STATE_UNSPECIFIED {
		t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING)
	}
}

// validateTenant verifies that the organization has a tenant assigned.
func (t *task) validateTenant() error {
	if !t.organization.HasMetadata() || t.organization.GetMetadata().GetTenant() == "" {
		return errors.New("Organization must have a tenant assigned")
	}
	return nil
}

// addFinalizer adds the controller finalizer to the organization if not already present.
// Returns true if the finalizer was added (indicating the update should be saved immediately).
func (t *task) addFinalizer() bool {
	if !t.organization.HasMetadata() {
		t.organization.SetMetadata(&privatev1.Metadata{})
	}
	list := t.organization.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.organization.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

// removeFinalizer removes the controller finalizer from the organization.
func (t *task) removeFinalizer() {
	if !t.organization.HasMetadata() {
		return
	}
	list := t.organization.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.organization.GetMetadata().SetFinalizers(list)
	}
}

// isBuiltin returns true if the organization is a builtin organization that should not be user-accessible in the
// identity provider. Builtin organizations like "shared" and "system" are created disabled.
func (t *task) isBuiltin() bool {
	name := t.organization.GetMetadata().GetName()
	return name == auth.SharedTenant || name == auth.SystemTenant
}

// delete performs the deletion cleanup for an organization.
func (t *task) delete(ctx context.Context) error {
	// Skip if not synced to IDP yet
	if t.organization.GetStatus().GetState() != privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED {
		t.removeFinalizer()
		return nil
	}

	// Delete from IDP
	orgName := t.organization.GetStatus().GetIdpOrganizationName()
	if orgName == "" {
		t.removeFinalizer()
		return nil
	}

	err := t.r.idpManager.DeleteOrganization(ctx, orgName)
	if err != nil {
		return fmt.Errorf("failed to delete IDP organization: %w", err)
	}

	t.r.logger.DebugContext(ctx, "Deleted organization from IDP",
		slog.String("organization_id", t.organization.GetId()),
		slog.String("idp_name", orgName),
	)

	t.removeFinalizer()
	return nil
}
