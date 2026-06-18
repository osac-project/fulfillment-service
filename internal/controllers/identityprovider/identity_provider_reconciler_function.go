/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package identityprovider

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
	idpClient  idp.Client
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

// SetIdpClient sets the IDP client that the reconciler will use to manage identity providers.
func (b *FunctionBuilder) SetIdpClient(value idp.Client) *FunctionBuilder {
	b.idpClient = value
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
	if b.idpClient == nil {
		err = errors.New("IDP client is mandatory")
		return
	}

	result = &function{
		logger:                  b.logger,
		identityProvidersClient: privatev1.NewIdentityProvidersClient(b.connection),
		idpClient:               b.idpClient,
		maskCalculator:          masks.NewCalculator().Build(),
	}
	return
}

// function is the implementation of the reconciler function.
type function struct {
	logger                  *slog.Logger
	identityProvidersClient privatev1.IdentityProvidersClient
	idpClient               idp.Client
	maskCalculator          *masks.Calculator
}

// Run executes the reconciliation logic for the given identity provider.
func (r *function) Run(ctx context.Context, identityProvider *privatev1.IdentityProvider) error {
	oldIdp := proto.Clone(identityProvider).(*privatev1.IdentityProvider)

	task := &task{
		r:                r,
		identityProvider: identityProvider,
	}

	var err error
	if identityProvider.HasMetadata() && identityProvider.GetMetadata().HasDeletionTimestamp() {
		err = task.delete(ctx)
	} else {
		err = task.update(ctx)
	}
	if err != nil {
		return err
	}

	updateMask := r.maskCalculator.Calculate(oldIdp, identityProvider)

	if len(updateMask.GetPaths()) > 0 {
		_, err = r.identityProvidersClient.Update(ctx, privatev1.IdentityProvidersUpdateRequest_builder{
			Object:     identityProvider,
			UpdateMask: updateMask,
		}.Build())
	}

	return err
}

// task contains the data needed to reconcile a single identity provider.
type task struct {
	r                *function
	identityProvider *privatev1.IdentityProvider
}

// update performs the reconciliation logic for creating or updating an identity provider.
func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	if err := t.validateTenant(); err != nil {
		return err
	}

	t.setDefaults()

	state := t.identityProvider.GetStatus().GetPhase()

	// Skip reconciliation for terminal error state
	if state == privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_ERROR {
		return nil
	}

	// For ready identity providers, no updates are needed
	if state == privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_READY {
		return nil
	}

	// Identity provider is UNSPECIFIED or UNKNOWN, perform initial sync to IDP
	return t.syncToIDP(ctx)
}

// syncToIDP synchronizes the identity provider to the IDP backend (Keycloak).
func (t *task) syncToIDP(ctx context.Context) error {
	// Build the IDP provider object from the spec
	// Use tenant-prefixed alias to ensure uniqueness across tenants in Keycloak
	alias := fmt.Sprintf("%s-%s", t.identityProvider.GetMetadata().GetTenant(), t.identityProvider.GetMetadata().GetName())
	idpProvider := &idp.IdentityProvider{
		Alias:       alias,
		DisplayName: t.identityProvider.GetSpec().GetTitle(),
		Type:        t.determineProviderType(),
		Enabled:     t.identityProvider.GetSpec().GetEnabled(),
		Config:      t.buildConfig(),
	}

	createdIdp, err := t.r.idpClient.CreateIdentityProvider(ctx, idpProvider)
	if err != nil {
		t.identityProvider.GetStatus().SetPhase(privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_ERROR)
		t.identityProvider.GetStatus().SetMessage(fmt.Sprintf("Identity provider creation in IDP failed: %v", err))
		return nil
	}

	t.identityProvider.GetStatus().SetPhase(privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_READY)
	t.identityProvider.GetStatus().SetMessage(fmt.Sprintf("Identity provider created successfully with alias: %s", createdIdp.Alias))

	t.r.logger.DebugContext(ctx, "Identity provider synced to IDP",
		slog.String("identity_provider_id", t.identityProvider.GetId()),
		slog.String("alias", createdIdp.Alias),
	)

	return nil
}

// determineProviderType returns the provider type based on which config is set.
func (t *task) determineProviderType() string {
	spec := t.identityProvider.GetSpec()
	if spec.HasLdap() {
		return "ldap"
	}
	if spec.HasOidc() {
		return "oidc"
	}
	return ""
}

// buildConfig builds the provider-specific configuration map.
func (t *task) buildConfig() map[string]string {
	config := make(map[string]string)
	spec := t.identityProvider.GetSpec()

	if ldap := spec.GetLdap(); ldap != nil {
		config["connectionUrl"] = ldap.GetConnectionUrl()
		config["bindDn"] = ldap.GetBindDn()
		config["bindCredential"] = ldap.GetBindCredential()
		config["usersDn"] = ldap.GetUsersDn()
	}

	if oidc := spec.GetOidc(); oidc != nil {
		config["authorizationUrl"] = oidc.GetAuthorizationUrl()
		config["tokenUrl"] = oidc.GetTokenUrl()
		config["clientId"] = oidc.GetClientId()
		config["clientSecret"] = oidc.GetClientSecret()
		config["issuer"] = oidc.GetIssuer()
	}

	return config
}

// validateTenant verifies that the identity provider has a valid tenant assigned.
// Identity providers must belong to a specific organization tenant, not "shared" or "system".
func (t *task) validateTenant() error {
	if !t.identityProvider.HasMetadata() || t.identityProvider.GetMetadata().GetTenant() == "" {
		return errors.New("Identity provider must have a tenant assigned") //nolint:staticcheck // ST1005: Identity provider is an API resource name
	}
	tenant := t.identityProvider.GetMetadata().GetTenant()
	if tenant == auth.SharedTenant || tenant == auth.SystemTenant {
		return fmt.Errorf("Identity provider cannot belong to '%s' tenant - must be scoped to a specific organization", tenant) //nolint:staticcheck // ST1005: Identity provider is an API resource name
	}
	return nil
}

// setDefaults sets default values for the identity provider.
func (t *task) setDefaults() {
	if !t.identityProvider.HasStatus() {
		t.identityProvider.SetStatus(&privatev1.IdentityProviderStatus{})
	}
	if t.identityProvider.GetStatus().GetPhase() == privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_UNSPECIFIED {
		t.identityProvider.GetStatus().SetPhase(privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_UNKNOWN)
	}
}

// addFinalizer adds the controller finalizer to the identity provider if not already present.
// Returns true if the finalizer was added (indicating the update should be saved immediately).
func (t *task) addFinalizer() bool {
	if !t.identityProvider.HasMetadata() {
		t.identityProvider.SetMetadata(&privatev1.Metadata{})
	}
	list := t.identityProvider.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.identityProvider.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

// removeFinalizer removes the controller finalizer from the identity provider.
func (t *task) removeFinalizer() {
	if !t.identityProvider.HasMetadata() {
		return
	}
	list := t.identityProvider.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.identityProvider.GetMetadata().SetFinalizers(list)
	}
}

// delete performs the deletion cleanup for an identity provider.
func (t *task) delete(ctx context.Context) error {
	// Skip if not in ready state (not synced to IDP yet)
	if t.identityProvider.GetStatus().GetPhase() != privatev1.IdentityProviderPhase_IDENTITY_PROVIDER_PHASE_READY {
		t.removeFinalizer()
		return nil
	}

	// TODO: Add DeleteIdentityProvider method to idp.Client interface and implement deletion
	// For now, we just remove the finalizer to allow the object to be deleted from the database
	t.r.logger.WarnContext(ctx, "Identity provider deletion from IDP not yet implemented",
		slog.String("identity_provider_id", t.identityProvider.GetId()),
	)

	t.removeFinalizer()
	return nil
}
