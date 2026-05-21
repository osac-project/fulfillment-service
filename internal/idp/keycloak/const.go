/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

// realmManagementClientID is the clientId of the built-in Keycloak client that contains
// all administrative roles for managing a realm. This client exists by default in every
// realm and is the only client we interact with for role assignments.
const realmManagementClientID = "realm-management"

// authorizationClientID is the clientId of the Keycloak client that has Authorization Services enabled.
// This client manages authorization resources for Projects and other protected resources.
const authorizationClientID = "authorino-uma"

// keycloakRealmManagementRoles are the standard Keycloak administrative roles.
//
// NOTE: These are CLIENT roles (Keycloak client roles), not organization roles.
// Keycloak implements administrative permissions as client-level roles on the built-in
// "realm-management" client that exists by default in every realm. There is no
// organization-level role equivalent for these admin permissions - they must be assigned
// using AssignClientRolesToUser with clientID "realm-management".
var keycloakRealmManagementRoles = []string{
	"manage-realm",
	"manage-users",
	"manage-clients",
	"manage-identity-providers",
	"manage-authorization",
	"manage-events",
	"view-realm",
	"view-users",
	"view-clients",
	"view-identity-providers",
	"view-authorization",
	"view-events",
}

// keycloakIdpManagerRoles are the limited Keycloak roles for the break-glass account.
// This account can manage users and identity providers but cannot modify realm settings,
// clients, or other critical configuration.
var keycloakIdpManagerRoles = []string{
	"manage-users",
	"view-users",
	"manage-identity-providers",
	"view-identity-providers",
	"view-realm",
}

// Authorization resource type constants define the types of protected resources.
const (
	// ResourceTypeProject is the type identifier for Project authorization resources
	ResourceTypeProject = "urn:osac:resources:project"
)
