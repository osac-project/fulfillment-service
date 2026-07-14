/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

//go:generate mockgen -source=../../api/osac/private/v1/projects_service_grpc.pb.go -destination=projects_client_mock.go -package=tenant ProjectsClient

package tenant

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
	idpManager *idp.TenantManager
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

// SetIdpManager sets the IDP manager that the reconciler will use to manage tenants in the identity provider.
func (b *FunctionBuilder) SetIdpManager(value *idp.TenantManager) *FunctionBuilder {
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
		logger:         b.logger,
		tenantsClient:  privatev1.NewTenantsClient(b.connection),
		projectsClient: privatev1.NewProjectsClient(b.connection),
		idpManager:     b.idpManager,
		maskCalculator: masks.NewCalculator().Build(),
	}
	return
}

// function is the implementation of the reconciler function.
type function struct {
	logger         *slog.Logger
	tenantsClient  privatev1.TenantsClient
	projectsClient privatev1.ProjectsClient
	idpManager     *idp.TenantManager
	maskCalculator *masks.Calculator
}

// Run executes the reconciliation logic for the given tenant.
func (r *function) Run(ctx context.Context, tenant *privatev1.Tenant) error {
	oldTenant := proto.Clone(tenant).(*privatev1.Tenant)

	task := &task{
		r:      r,
		tenant: tenant,
	}

	var err error
	if tenant.HasMetadata() && tenant.GetMetadata().HasDeletionTimestamp() {
		err = task.delete(ctx)
	} else {
		err = task.update(ctx)
	}
	if err != nil {
		return err
	}

	updateMask := r.maskCalculator.Calculate(oldTenant, tenant)

	if len(updateMask.GetPaths()) > 0 {
		_, err = r.tenantsClient.Update(ctx, privatev1.TenantsUpdateRequest_builder{
			Object:     tenant,
			UpdateMask: updateMask,
		}.Build())
	}

	return err
}

// task contains the data needed to reconcile a single tenant.
type task struct {
	r      *function
	tenant *privatev1.Tenant
}

// update performs the reconciliation logic for creating or updating a tenant.
func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	t.setDefaults()

	if err := t.validateTenant(); err != nil {
		return err
	}

	state := t.tenant.GetStatus().GetState()

	// Skip reconciliation only for terminal failure state.
	// This prevents infinite retry loops when IDP operations fail.
	if state == privatev1.TenantState_TENANT_STATE_FAILED {
		return nil
	}

	// For synced tenants, no updates are needed since spec is empty.
	if state == privatev1.TenantState_TENANT_STATE_SYNCED {
		return t.updateIDP(ctx)
	}

	// Tenant is PENDING or UNSPECIFIED, perform initial sync to IDP
	return t.syncToIDP(ctx)
}

// syncToIDP synchronizes the tenant to the identity provider.
func (t *task) syncToIDP(ctx context.Context) error {
	if t.tenant.GetStatus().GetIdpTenantName() != "" {
		t.tenant.GetStatus().SetState(privatev1.TenantState_TENANT_STATE_SYNCED)
		t.tenant.GetStatus().ClearMessage()
		return nil
	}

	t.tenant.GetStatus().SetState(privatev1.TenantState_TENANT_STATE_PENDING)

	tenantName := t.tenant.GetMetadata().GetName()
	config := &idp.TenantConfig{
		Name:    tenantName,
		Enabled: new(!t.isBuiltin()),
		Domains: t.tenant.GetSpec().GetDomains(),
	}

	credentials, err := t.r.idpManager.CreateTenant(ctx, config)
	if err != nil {
		t.tenant.GetStatus().SetState(privatev1.TenantState_TENANT_STATE_FAILED)
		t.tenant.GetStatus().SetMessage(fmt.Sprintf("Tenant creation in IDP failed: %v", err))
		return nil
	}

	t.tenant.GetStatus().SetState(privatev1.TenantState_TENANT_STATE_SYNCED)
	t.tenant.GetStatus().SetIdpTenantName(config.Name)
	t.tenant.GetStatus().SetBreakGlassUserId(credentials.UserID)

	breakGlassCredentials := privatev1.BreakGlassCredentials_builder{
		Username: credentials.Username,
		Password: credentials.Password,
	}.Build()
	t.tenant.GetStatus().SetBreakGlassCredentials(breakGlassCredentials)

	t.r.logger.DebugContext(ctx, "Tenant synced to IDP",
		slog.String("tenant_id", t.tenant.GetId()),
		slog.String("tenant_name", tenantName),
	)

	return nil
}

// updateIDP updates the tenant in the identity provider with the current spec values.
func (t *task) updateIDP(ctx context.Context) error {
	tenantName := t.tenant.GetStatus().GetIdpTenantName()
	if tenantName == "" {
		t.tenant.GetStatus().SetState(privatev1.TenantState_TENANT_STATE_FAILED)
		t.tenant.GetStatus().SetMessage("Tenant name is empty")
		t.r.logger.ErrorContext(
			ctx,
			"Tenant name is empty",
			slog.String("tenant", t.tenant.GetMetadata().GetName()),
		)
		return nil
	}
	domains := t.tenant.GetSpec().GetDomains()
	err := t.r.idpManager.UpdateTenant(ctx, tenantName, domains)
	if err != nil {
		t.r.logger.ErrorContext(ctx, "Failed to update tenant domains in IDP",
			slog.String("tenant_id", t.tenant.GetId()),
			slog.Any("error", err),
		)
		return err
	}
	return nil
}

// setDefaults sets default values for the tenant.
func (t *task) setDefaults() {
	if !t.tenant.HasStatus() {
		t.tenant.SetStatus(&privatev1.TenantStatus{})
	}
	if t.tenant.GetStatus().GetState() == privatev1.TenantState_TENANT_STATE_UNSPECIFIED {
		t.tenant.GetStatus().SetState(privatev1.TenantState_TENANT_STATE_PENDING)
	}
}

// validateTenant verifies that the tenant has a tenant assigned.
func (t *task) validateTenant() error {
	if !t.tenant.HasMetadata() || t.tenant.GetMetadata().GetTenant() == "" {
		return errors.New("Tenant must have a metadata.tenant assigned") //nolint:staticcheck // ST1005: Tenant is an API resource name
	}
	return nil
}

// addFinalizer adds the controller finalizer to the tenant if not already present.
// Returns true if the finalizer was added (indicating the update should be saved immediately).
func (t *task) addFinalizer() bool {
	if !t.tenant.HasMetadata() {
		t.tenant.SetMetadata(&privatev1.Metadata{})
	}
	list := t.tenant.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.tenant.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

// removeFinalizer removes the controller finalizer from the tenant.
func (t *task) removeFinalizer() {
	if !t.tenant.HasMetadata() {
		return
	}
	list := t.tenant.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.tenant.GetMetadata().SetFinalizers(list)
	}
}

// isBuiltin returns true if the tenant is a builtin tenant that should not be user-accessible in the
// identity provider. Builtin tenants like "shared" and "system" are created disabled.
func (t *task) isBuiltin() bool {
	name := t.tenant.GetMetadata().GetName()
	return name == auth.SharedTenant || name == auth.SystemTenant
}

// delete performs the deletion cleanup for a tenant.
func (t *task) delete(ctx context.Context) error {
	// Block until all projects are deleted by the administrator.
	remaining, err := t.countRemainingProjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to query remaining projects: %w", err)
	}
	if remaining > 0 {
		t.r.logger.InfoContext(ctx, "Waiting for projects to be deleted before tenant can be removed",
			slog.String("tenant_id", t.tenant.GetId()),
			slog.Int("remaining_projects", int(remaining)),
		)
		return fmt.Errorf("tenant still has %d project(s) pending deletion", remaining)
	}

	// Skip if not synced to IDP yet
	if t.tenant.GetStatus().GetState() != privatev1.TenantState_TENANT_STATE_SYNCED {
		t.removeFinalizer()
		return nil
	}

	// Delete from IDP
	tenantName := t.tenant.GetStatus().GetIdpTenantName()
	if tenantName == "" {
		t.removeFinalizer()
		return nil
	}

	err = t.r.idpManager.DeleteTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to delete IDP tenant: %w", err)
	}

	t.r.logger.DebugContext(ctx, "Deleted tenant from IDP",
		slog.String("tenant_id", t.tenant.GetId()),
		slog.String("idp_name", tenantName),
	)

	t.removeFinalizer()
	return nil
}

// countRemainingProjects returns the number of projects that still belong to
// this tenant. The tenant reconciler blocks deletion until this returns 0 —
// it is the administrator's responsibility to delete all projects first.
func (t *task) countRemainingProjects(ctx context.Context) (int32, error) {
	listFilter := fmt.Sprintf("this.metadata.tenant == %q", t.tenant.GetMetadata().GetName())
	listResp, err := t.r.projectsClient.List(ctx, privatev1.ProjectsListRequest_builder{
		Filter: new(listFilter),
		Limit:  new(int32(0)),
	}.Build())
	if err != nil {
		return 0, err
	}
	return listResp.GetTotal(), nil
}
