# Keycloak Setup and Configuration Guide

This guide explains how to set up Keycloak as an Identity Provider (IDP) broker for the fulfillment service
and configure the necessary mappings and authorization rules.

> **Note**: The fulfillment service uses Keycloak as the identity provider broker.
> The Keycloak setup requires:
>
> - A valid OAuth issuer URL
> - JWT tokens containing a username claim (`preferred_username` or `username`) and optionally
>   tenant claims (`organization`, `organizations`, or `groups`)
> - The ability to validate tokens using the issuer's public keys
>


## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Installing Keycloak](#installing-keycloak)
3. [Keycloak Configuration](#keycloak-configuration)
4. [Fulfillment Service Configuration](#fulfillment-service-configuration)
5. [User and Group Mapping](#user-and-group-mapping)
6. [Tenancy Logic](#tenancy-logic)
7. [Authorization Configuration](#authorization-configuration)
8. [Authorization Flow](#authorization-flow)
9. [Verification](#verification)
10. [Troubleshooting](#troubleshooting)

## Prerequisites

Before installing Keycloak, ensure you have:

- A Kubernetes cluster (Kind or OpenShift)
- [cert-manager](https://cert-manager.io/) operator installed
- At least one cert-manager issuer configured (ClusterIssuer or Issuer)
## Installing Keycloak

The fulfillment service includes a Helm chart for deploying Keycloak. The chart is published with
each release as an OCI image at [ghcr.io/osac/charts/keycloak][keycloak-chart].

[keycloak-chart]: https://github.com/osac-project/fulfillment-service/pkgs/container/charts%2Fkeycloak

### Installation Steps

Install Keycloak using the published Helm chart from the OCI registry:

**For OpenShift clusters:**

```bash
helm install keycloak oci://ghcr.io/osac/charts/keycloak \
  --version 0.0.27 \
  --namespace keycloak \
  --create-namespace \
  --set variant=openshift \
  --set hostname=keycloak.keycloak.svc.cluster.local \
  --set certs.issuerRef.name=default-ca \
  --wait
```

**For Kind clusters:**

```bash
helm install keycloak oci://ghcr.io/osac/charts/keycloak \
  --version 0.0.27 \
  --namespace keycloak \
  --create-namespace \
  --set variant=kind \
  --set hostname=keycloak.keycloak.svc.cluster.local \
  --set certs.issuerRef.name=default-ca \
  --wait
```

Replace `0.0.27` with the version you want to use. You can find available versions at the [chart
registry][keycloak-chart].

**Using a values file (optional):**

You can also create a `keycloak-values.yaml` file with your configuration:

```yaml
variant: openshift  # or "kind" for Kind clusters

hostname: keycloak.keycloak.svc.cluster.local

certs:
  issuerRef:
    kind: ClusterIssuer  # or "Issuer" for namespace-scoped issuer
    name: default-ca     # Replace with your cert-manager issuer name

```

Then install with:

```bash
helm install keycloak oci://ghcr.io/osac/charts/keycloak \
  --version 0.0.27 \
  --namespace keycloak \
  --create-namespace \
  --values keycloak-values.yaml \
  --wait
```

### Verify the Installation

   ```bash
   kubectl get pods -n keycloak
   kubectl get svc -n keycloak
   ```

   Wait until the Keycloak pod is in `Running` state and ready.

### Keycloak Configuration Parameters

| Parameter | Description | Required | Default |
|-----------|-------------|----------|---------|
| `variant` | Deployment variant (`openshift` or `kind`) | No | `kind` |
| `hostname` | The hostname that Keycloak uses to refer to itself | **Yes** | None |
| `certs.issuerRef.kind` | The kind of cert-manager issuer (`ClusterIssuer` or `Issuer`) | No | `ClusterIssuer` |
| `certs.issuerRef.name` | The name of the cert-manager issuer for TLS certificates | **Yes** | None |
| `images.keycloak` | The Keycloak container image | No | `quay.io/keycloak/keycloak:26.3` |
| `images.postgres` | The PostgreSQL container image | No | `quay.io/sclorg/postgresql-18-c10s:latest` |

### Important Notes

- The `hostname` parameter is critical and must match the hostname that Keycloak will use to
  reference itself, meaning that when Keycloak redirects users, or generates an URL for itself, it
  will use this host name. This is also used for token issuer URLs.

- The default admin credentials are:
  - Username: `admin`
  - Password: `admin`
  - **Important**: Change these credentials in production environments!

## Keycloak Configuration

The Helm chart includes a pre-configured realm named `osac` with the following setup:

### Realm Configuration

- **Realm Name**: `osac`

### Pre-configured Clients

The realm includes the **osac-cli** (Public) client:
   - Client ID: `osac-cli`
   - Type: Public client (no client secret required)
   - Enabled flows: Standard flow (authorization code), Direct access grants, Device authorization
   - PKCE: S256
   - Use case: Command-line interface authentication (via `osac login`)
   - Default client scopes: `groups`, `basic`, `username`, `organization`, `roles`, `osac-api`

### Pre-configured Realm Roles

The realm includes the following roles used by the authorization policy:

| Role | Description |
|------|-------------|
| `tenant-admin` | Tenant administrator with full user and IdP management permissions |
| `tenant-user-manager` | Tenant user manager with user management permissions |
| `tenant-idp-manager` | Tenant IdP manager with IdP configuration and role assignment permissions |

Assign these roles to users via the Keycloak Admin Console under **Users** → Select user →
**Role mapping** tab.

### Token Audience (`aud` Claim)

The realm includes an `osac-api` client scope that adds a custom audience claim to access tokens
using an `oidc-audience-mapper`. This ensures tokens issued for the `osac-cli` client contain
`"aud": "osac-api"` in their payload. The scope is included in the `osac-cli` client's default
scopes, so no additional configuration is needed.

### Keycloak Organizations

The realm has Keycloak Organizations enabled (`organizationsEnabled: true`). When organizations are
configured, the `organization` claim is included in JWT tokens via the `oidc-organization-membership-mapper`.
This claim is used by the authorization policy to determine the user's tenant (see [Subject
Resolution](#subject-resolution)).

### Accessing Keycloak Admin Console

To access the Keycloak Admin Console:

1. **Access the console**:

   Add the host name used internally in the cluster pointing to `127.0.0.1` to `/etc/hosts`:

   ```
   127.0.0.1 keycloak.keycloak.svc.cluster.local
   ```

   Open your browser and navigate to https://keycloak.keycloak.svc.cluster.local:8000, then accept
   the self-signed certificate warning.

2. **Login**:

   - Username: `admin`
   - Password: `admin`

3. **Select the realm**:

   - Click on the realm dropdown (top left)
   - Select `osac`

### Exporting Realm Configuration

If you need to export the realm configuration for backup or modification:

1. Find the Keycloak pod:

   ```bash
   pod=$(
      kubectl get pods -n keycloak -l app=keycloak-service -o json |
      jq -r '.items[].metadata.name'
   )
   ```

2. Export the realm:

   ```bash
   kubectl exec -n keycloak "${pod}" -- /opt/keycloak/bin/kc.sh export --realm osac --file /tmp/realm.json
   ```

3. Copy the exported file:

   ```bash
   kubectl exec -n keycloak "${pod}" -- cat /tmp/realm.json > realm.json
   ```

4. (Optional) Update the chart's realm file:

   ```bash
   cp realm.json it/charts/keycloak/files/realm.json
   ```

## Fulfillment Service Configuration

After Keycloak is installed, you need to configure the fulfillment service to use Keycloak as its
identity provider.

### 1. Configure the Issuer URL

The fulfillment service needs to know the Keycloak issuer URL to validate JWT tokens. The issuer URL
format is:

```
https://<keycloak-hostname>:<port>/realms/<realm-name>
```

For example:
```
https://keycloak.keycloak.svc.cluster.local:8000/realms/osac
```

### 2. Update the Fulfillment Service Deployment

When installing the fulfillment service using the Helm chart, set the `auth.issuerUrl` parameter:

```bash
helm install fulfillment-service oci://ghcr.io/osac/charts/fulfillment-service \
  --version 0.0.27 \
  --namespace osac \
  --create-namespace \
  --set variant=kind \
  --set hostname=fulfillment-api.osac.cluster.local \
  --set certs.issuerRef.name=default-ca \
  --set certs.caBundle.configMap=ca-bundle \
  --set auth.issuerUrl=https://keycloak.keycloak.svc.cluster.local:8000/realms/osac \
  --wait
```

Or in a values file:

```yaml
auth:
  issuerUrl: https://keycloak.keycloak.svc.cluster.local:8000/realms/osac
```

### 3. Update the Server Configuration

The fulfillment service server component also needs to be configured with the trusted token issuer.
This is done via the `--grpc-authn-trusted-token-issuers` flag in the deployment.

The Helm chart automatically sets this from the `auth.issuerUrl` value. In the deployment, you'll
see:

```yaml
- --grpc-authn-trusted-token-issuers=https://keycloak.keycloak.svc.cluster.local:8000/realms/osac
```

### 4. Configure Controller Credentials

The fulfillment service controller authenticates to the API using the OAuth client credentials flow.
Configure the controller credentials in the Helm chart values:

```yaml
auth:
  controllerCredentials:
  - secret:
      name: fulfillment-controller-credentials
      items:
      - key: client-id
        param: client-id
      - key: client-secret
        param: client-secret
```

The controller's service account (`service-account-osac-controller`) is listed as an admin service
account in the authorization policy, granting it full access to all API methods.

> **Note**: The fulfillment service no longer uses Kubernetes service accounts (`controller` or
> `client`) for internal authentication. All service-to-service communication now uses OAuth client
> credentials.

### 5. Configure Identity Provider (IDP) Management

The fulfillment service manages organizations in the identity provider via the organization
reconciler. This requires a separate set of credentials with IDP admin permissions:

```yaml
idp:
  provider: keycloak
  url: https://keycloak.keycloak.svc.cluster.local:8000
  credentials:
  - secret:
      name: fulfillment-controller-credentials
      items:
      - key: client-id
        param: client-id
      - key: client-secret
        param: client-secret
```

The IDP credentials must have sufficient privileges to manage organizations and users (for
Keycloak, this means the `realm-management` client roles: `manage-realm`, `manage-users`,
`view-realm`, `view-users`). You can reuse the same credentials as `auth.controllerCredentials`
if the service account has both API access and IDP management permissions.

### 6. Trusted Token Issuers

The fulfillment service maintains a single list of trusted token issuers, configured via the
`--grpc-authn-trusted-token-issuers` server flag. This same list is used in two places:

1. **Server-side authentication**: The server validates that incoming JWT tokens were issued by one
   of the trusted issuers.

2. **Client discovery**: The Capabilities API (`Capabilities/Get`) returns the trusted issuers list
   in the `authn.trusted_token_issuers` field. Clients like `osac login` use this to auto-select
   which OAuth issuer to authenticate against, without requiring the user to specify one explicitly.

## User and Group Mapping

The fulfillment service maps users and groups from Keycloak to its internal user
and tenant concepts.

### Key Concepts

- **Users**: Represent individual authenticated entities (users or service accounts)
- **Tenants**: Represent groups of users. In Keycloak, these map to **Organizations**, but the fulfillment
  service refers to them as **tenants**
- **Organizations**: The fulfillment service does **not** have an explicit "organization" concept.
  Organizations and tenants are defined and managed in the external identity provider (Keycloak)
- **Projects**: Represents hierarchical directories within a **tenant**. It groups objects together, and provides
  a boundary to allow certain groups of users to view those objects.

### Subject Resolution

The mapping from authentication details to the fulfillment service's internal `user` and `tenants`
fields is performed by the service's built-in authentication and authorization interceptors.

For JWT-authenticated users, the `user` is taken from the `preferred_username` claim (falling back
to `username` if not present). The `tenants` are resolved in the following priority order:

1. **`organization` claim** — from Keycloak Organizations (when the user belongs to an organization)
2. **`organizations` claim** — plural form (alternative format)
3. **`groups` claim** — fallback when neither organization claim is present

For Kubernetes service accounts, the namespace is used as the tenant and the short service account
name as the user.

For admin users (those matching `emergency_service_accounts`, `admin_service_accounts`, or
`admin_groups`), the tenants are set to `["*"]` (universal), granting visibility to all tenants.

The authorization rules are implemented via the built-in Rego policy in the gRPC authorization
interceptor.

### Configuring Keycloak to Include Groups in Tokens

To ensure that user groups are included in the JWT tokens issued by Keycloak:

1. **Access the Keycloak Admin Console**
   (see [Accessing Keycloak Admin Console](#accessing-keycloak-admin-console))

2. **Navigate to the Client**:
   - Go to **Clients** → Select your client (e.g., `osac-cli`)

3. **Configure Client Scopes**:
   - Go to **Client scopes** tab
   - Ensure the `groups` scope is assigned to the client
   - Or create a custom mapper to include groups in the token

4. **Create a Group Mapper** (if needed):
   - Go to **Client scopes** → `groups` → **Mappers** tab
   - Click **Add mapper** → **By configuration**
   - Select **Group Membership** mapper
   - Configure:
     - **Name**: `groups`
     - **Token Claim Name**: `groups`
     - **Full group path**: `false` (or `true` if you want full
       paths like `/tenant-a/team-1`)
     - **Add to access token**: `true`
     - **Add to ID token**: `true` (if needed)

5. **Assign Users to Groups**:
   - Go to **Users** → Select a user → **Groups** tab
   - Assign the user to the appropriate groups (these will become tenants)

## Tenancy Logic

The fulfillment service implements a tenancy logic that manages how resources are associated with
tenants and which resources users can access. The tenancy logic operates with three distinct
concepts:

1. **Assignable Tenants**: The set of tenants that can be assigned to a resource, either explicitly
   by the user or automatically as defaults. This represents the complete set of valid tenant
   assignments for the user's context. Note that some assignable tenants may be invisible to the
   user, meaning the user cannot explicitly select them, but they could still be assigned by
   default.

2. **Default Tenant**: The tenant that will be automatically assigned to a resource when the user
   creates it without explicitly specifying a tenant. The default tenant is always one of the
   assignable tenants.

3. **Visible Tenants**: The tenants from which a user can see resources. When listing or querying
   resources, only those belonging to visible tenants will be returned. Users can only explicitly
   assign a tenant that is both assignable and visible to them.

The tenancy logic can be configured using the `--tenancy-logic` command-line flag when starting the
fulfillment service. Valid values are `default` and `guest`.

### Additional Tenancy Concepts

1. **Shared Tenant**: The `shared` tenant is a special tenant that is always included in the visible
   tenants for all users. Resources assigned to the `shared` tenant are visible to **everyone**.
   This is useful for templates, shared configurations, or other resources that should be accessible
   across all tenants.

2. **System Tenant**: The `system` tenant is a special tenant used for objects that are only visible
   to the system itself. Resources assigned to the `system` tenant are not visible to regular users.
   This is used internally for system-level resources.

3. **Multi-Tenant Users**: A user can belong to multiple tenants. This is configured in Keycloak by
   assigning the user to multiple groups. The fulfillment service will reflect this in the
   assignable and visible tenant sets. However, each resource is assigned to exactly one tenant.

4. **Tenant Assignment**: When a user creates a resource:

   - The user is recorded in the `metadata.creator` field of the object, and is purely informative.
     The system doesn't currently use it to make any authorization or visibility decisions.
   - If the user explicitly specifies a tenant, that tenant is assigned. The user can only
     explicitly assign a tenant that is both assignable and visible.
   - If the user doesn't specify a tenant, the default tenant is automatically assigned.
   - Tenant assignment is recorded in the `metadata.tenant` field and is used by the server to make
     visibility decisions.

5. **Tenant Visibility**: When a user queries resources, the visible tenants determine what they can
   see. A user can only see a resource if the resource's assigned tenant is one of the user's
   visible tenants.

### Tenancy Logic Implementations

The following tenancy logic implementations are available:

#### Default

Use `--tenancy-logic=default` (this is the default). This implementation reads the tenants directly
from the `tenants` field of the subject, which the authentication interceptor populates from the
JWT token claims (see [Subject Resolution](#subject-resolution)).

- **Assignable Tenants**: All tenants from the subject
- **Default Tenant**: First tenant from the subject. When the subject has access to all tenants
  (e.g., an admin with the universal set `["*"]`), the default tenant is `shared` because a
  universal set cannot be stored as the tenant of an object.
- **Visible Tenants**: All subject's tenants plus the `shared` tenant. For admins with the
  universal set, all tenants are visible.

Example with a JWT user:
- User `alice` belongs to groups: `["team-a", "team-b"]`
- The service maps these groups into tenants: `["team-a", "team-b"]`
- Assignable tenants: `["team-a", "team-b"]`
- Default tenant: `"team-a"`
- When `alice` creates a cluster without specifying a tenant:
  - The cluster is assigned to tenant: `"team-a"`
- When `alice` lists clusters:
  - She can see clusters from: `["team-a", "team-b", "shared"]`

Example with a service account:
- Service account `system:serviceaccount:osac:client`
- The service extracts the namespace `osac` as the tenant: `["osac"]`
- Assignable tenants: `["osac"]`
- Default tenant: `"osac"`
- When creating a resource without specifying a tenant:
  - Assigned to tenant: `"osac"`
- When listing resources:
  - Can see resources from: `["osac", "shared"]`

#### Guest

Use `--tenancy-logic=guest` for guest user access:

- **Assignable Tenants**: The `guest` tenant only
- **Default Tenant**: The `guest` tenant
- **Visible Tenants**: The `guest` tenant plus the `shared` tenant

This is intended only for development and testing environments, in combination with the `guest`
authentication function.

Example:
- Any user (authenticated or guest)
- Assignable tenants: `["guest"]`
- Default tenant: `"guest"`
- When creating a resource without specifying a tenant:
  - Assigned to tenant: `"guest"`
- When listing resources:
  - Can see resources from: `["guest", "shared"]`

### Configuring Tenancy in Keycloak

To configure multi-tenant access in Keycloak:

1. **Create Groups** (these become tenants):
   - Go to **Groups** → **Create group**
   - Name the group (e.g., `team-a`, `tenant-1`, `organization-1`)
   - Create as many groups as needed

2. **Assign Users to Groups**:
   - Go to **Users** → Select a user → **Groups** tab
   - Click **Join group** and select the groups the user should belong to
   - A user can belong to multiple groups

3. **Configure Group Mapper** (as described in [Configuring
   Keycloak to Include Groups in
   Tokens](#configuring-keycloak-to-include-groups-in-tokens))

### Future Enhancements

Future enhancements may include:
- Support for an additional "organization" layer (requiring development)
- Custom tenant naming conventions
- Additional tenancy logic implementations for specific use cases

## Authorization Configuration

The fulfillment service uses Open Policy Agent (OPA) Rego policies for authorization. The
authorization rules are built into the service's gRPC authorization interceptor. The defined rules are a very simple
set intended for development and testing purposes. Further rules and policies can be configured
according to the different needs.

### Authorization Rules Overview

The authorization policy distinguishes between the following subject categories:

1. **Admin Subjects**: Users with full access to all API methods. An account is considered admin if
   it matches any of:
   - **Emergency service accounts**: Kubernetes service accounts for emergency access when the
     identity provider is unavailable (e.g., `system:serviceaccount:<namespace>:admin`)
   - **Admin service accounts**: OAuth service accounts registered as admins
     (e.g., `service-account-osac-admin`, `service-account-osac-controller`)
   - **Admin groups**: Users belonging to admin groups (e.g., the `admins` group)

2. **Tenant Admin Subjects**: Users with the `tenant-admin` or `tenant-user-manager` realm role.
   They inherit all client permissions **plus** access to user management methods
   (`Users/Create`, `Users/Get`, `Users/List`, `Users/Update`, `Users/Delete`).

3. **Tenant IdP Manager Subjects**: Users with the `tenant-admin` or `tenant-idp-manager` realm
   role. They inherit all client permissions. When IdP management APIs are implemented, they will
   also gain access to those methods.

4. **Client Subjects**: All other authenticated users (JWT tokens from Keycloak that are not admin, tenant-admin, or tenant-idp-manager).

### Authorization Logic

The authorization policy allows:

1. **Everyone** (authenticated users):
   - Metadata endpoints (`/metadata.*`)
   - gRPC reflection endpoints (`/grpc.reflection.*`)
   - Health check endpoints (`/grpc.health.*`)

2. **Client Users** (and tenant admins / IdP managers who inherit client permissions):
   - Specific gRPC methods for:
     - Clusters: `Create`, `Delete`, `Get`, `GetKubeconfig`,
       `GetKubeconfigViaHttp`, `GetPassword`,
       `GetPasswordViaHttp`, `List`, `Update`
     - Cluster Templates: `Get`, `List`
     - Cluster Catalog Items: `Get`, `List`
     - Compute Instances: `Create`, `Delete`, `Get`, `List`, `Update`
     - Compute Instance Templates: `Get`, `List`
     - Console Sessions: `Create`
     - Events: `Watch`
     - Host Types: `Get`, `List`
     - Network Classes: `Create`, `Delete`, `Get`, `List`, `Update`
     - Public IP Attachments: `Create`, `Delete`, `Get`, `List`, `Update`
     - Public IPs: `Create`, `Delete`, `Get`, `List`, `Update`
     - Role Bindings: `Get`, `List`
     - Roles: `Get`, `List`
     - Security Groups: `Create`, `Delete`, `Get`, `List`, `Update`
     - Subnets: `Create`, `Delete`, `Get`, `List`, `Update`
     - Virtual Networks: `Create`, `Delete`, `Get`, `List`, `Update`

3. **Tenant Admins** (in addition to client permissions):
   - Users: `Create`, `Get`, `List`, `Update`, `Delete`

4. **Admin Users**:
   - All methods (full access)

### Customizing Authorization Rules

To modify authorization rules, edit the Rego policy embedded in the authorization interceptor at
`internal/auth/`. Example: To add a new allowed method for client users, add it to the
`has_client_permissions` rule:

```rego
allow {
  has_client_permissions
  grpc_method in {
    # ... existing methods ...
    "/osac.public.v1.NewService/NewMethod",
  }
}
```

Example: To add a new emergency service account:

```rego
emergency_service_accounts := {
  "system:serviceaccount:osac:admin",
  "system:serviceaccount:osac:new-admin",
}
```

Example: To add a new admin OAuth service account:

```rego
admin_service_accounts := {
  "service-account-osac-admin",
  "service-account-osac-controller",
  "service-account-new-admin",
}
```

After modifying the authorization rules, rebuild and redeploy the service.

## Authorization Flow

The fulfillment service uses built-in gRPC interceptors for authentication and authorization:

1. **Authentication interceptor**: Validates JWT tokens using JWKS endpoints discovered from
   the trusted token issuers.
2. **Authorization interceptor**: Evaluates Rego policies to check whether the operation is
   allowed for the authenticated user.
3. **Tenancy logic**: Filters resource access based on the user's tenant membership.

### Step-by-Step Authorization Process

1. **User Authentication**:
   - User logs in through Keycloak using `osac login`
   - Receives a JWT access token containing:
     - Username (`preferred_username` claim, falling back to `username`)
     - Tenant source (resolved in priority order): `organization` claim, then `organizations`,
       then `groups` as a fallback
     - Realm roles (`realm_access.roles`) — used for tenant-admin / IdP-manager authorization
     - Audience (`aud: "osac-api"`) — API audience claim

2. **Request Initiation**:
   - User makes a request to the fulfillment service API
   - Includes the JWT token in the
     `Authorization: Bearer <token>` header
3. **Authentication**:
   - The authentication interceptor validates the JWT token signature and expiration
   - Extracts user identity and tenants from token claims

4. **Authorization**:
   - The authorization interceptor evaluates the Rego policy
   - Checks if the **operation type** is allowed for this user
   - **If authorized**: Request proceeds to the server handler
   - **If denied**: Returns `PERMISSION_DENIED` to the user

5. **Tenancy Validation**:
   - The service applies tenancy logic to determine:
       - **Assignable tenants**: Which tenants can be assigned to resources
       - **Default tenant**: Which tenant to assign if not explicitly specified
       - **Visible tenants**: Which tenants the user can query
         resources from
     - Validates access to specific resources
     - Performs the operation or returns an error

### Example Authorization Scenarios

#### Scenario 1: Client User Creating a Cluster

1. User `alice` (belongs to groups: `["team-a"]`) authenticates with Keycloak
2. `alice` sends: `POST /api/fulfillment/v1/clusters` with JWT token
3. **Authorization checks**:
   - Is `alice` authenticated? ✅ Yes
   - Is `Create` operation allowed for client users? ✅ Yes
   - **Result**: Authorized ✅
4. **Tenancy**:
   - User: `alice`, tenants: `["team-a"]`
   - Determines assignable tenants: `["team-a"]`, default tenant:
     `"team-a"`, visible tenants: `["team-a", "shared"]`
   - No tenant specified, so assigns the default tenant: `"team-a"`
   - **Result**: Cluster created ✅

#### Scenario 2: Client User Accessing Admin-Only Method

1. User `alice` sends: `POST /api/fulfillment/v1/admin-only-method` with JWT token
2. **Authorization checks**:
   - Is `alice` authenticated? ✅ Yes
   - Is `admin-only-method` allowed for client users? ❌ No
   - Is `alice` an admin? ❌ No
   - **Result**: Denied ❌
3. **User receives**: `PERMISSION_DENIED`

#### Scenario 3: Admin User Accessing Any Method

1. Service account `service-account-osac-admin` sends request with OAuth client credentials token
2. **Authorization checks**:
   - Is service account authenticated? ✅ Yes
   - Is the subject in `admin_service_accounts`? ✅ Yes
   - **Result**: Authorized ✅ (admins can access everything)
3. **Tenancy**:
   - Tenants: `["*"]` (universal)
   - Processes the request with full access to all tenants
   - **Result**: Operation succeeds ✅

#### Scenario 3b: Tenant Admin Managing Users

1. User `bob` (realm roles: `["tenant-admin"]`, organization: `["team-a"]`) sends:
   `POST /osac.public.v1.Users/Create` with JWT token
2. **Authorization checks**:
   - Is `bob` authenticated? ✅ Yes
   - Does `bob` have `tenant-admin` role? ✅ Yes
   - Is `Users/Create` allowed for tenant admins? ✅ Yes
   - **Result**: Authorized ✅
3. **Tenancy**:
   - User: `bob`, tenants: `["team-a"]`
   - Validates that `bob` can only manage users within tenant `team-a`
   - **Result**: User created in tenant `team-a` ✅

#### Scenario 4: User Viewing Resources (Tenancy Filtering)

1. User `alice` (tenants: `["team-a"]`) sends: `GET /api/fulfillment/v1/clusters`
2. **Authorization checks**:
   - Is `List` operation allowed? ✅ Yes
   - **Result**: Authorized ✅
3. **Tenancy**:
   - Determines visible tenants: `["team-a", "shared"]`
   - Queries database filtering to only return clusters from these
     tenants
   - **Result**: Returns only clusters from `team-a` and `shared`
     tenants ✅

### Roles and Permissions

The fulfillment service uses Keycloak realm roles for role-based authorization. The Rego policy
reads roles from the `realm_access.roles` claim in JWT tokens.

1. **Admin Users**:
   - Emergency K8s service accounts (e.g., `system:serviceaccount:<namespace>:admin`)
   - OAuth admin service accounts (e.g., `service-account-osac-admin`, `service-account-osac-controller`)
   - Users in admin groups (e.g., the `admins` group)
   - Have full access to all operations

2. **Tenant Admin Users**:
   - Users with the `tenant-admin` or `tenant-user-manager` realm role
   - Have all client permissions plus user management (`Users/Create`, `Get`, `List`, `Update`, `Delete`)
   - Access is restricted by tenancy (can only manage users within their tenants)

3. **Tenant IdP Manager Users**:
   - Users with the `tenant-admin` or `tenant-idp-manager` realm role
   - Have all client permissions (IdP management APIs will be added in the future)

4. **Client Users**:
   - All other authenticated users
   - Have access to a specific list of operations (defined in the Rego policy)
   - Access is further restricted by tenancy (can only see resources from their tenants)

### Applying Roles in Keycloak

The pre-configured realm includes the roles needed by the authorization policy. To assign roles:

1. **Assign Roles to Users**:
   - Go to **Users** → Select user → **Role mapping** tab
   - Click **Assign role**
   - Select one of: `tenant-admin`, `tenant-user-manager`, `tenant-idp-manager`

2. **Roles are included in tokens automatically**: The `roles` client scope (included in
   `osac-cli` default scopes) maps realm roles into the `realm_access.roles` claim in access
   tokens. No additional mapper configuration is needed.

## Verification

### 1. Verify Keycloak is Running

```bash
kubectl get pods -n keycloak
kubectl get svc -n keycloak
```

### 2. Verify Keycloak Realm

Access the Keycloak Admin Console and verify:
- The `osac` realm exists
- The `osac-cli` client is configured with the correct default scopes
- Organizations are enabled
- Realm roles (`tenant-admin`, `tenant-user-manager`, `tenant-idp-manager`) exist

### 3. Verify Fulfillment Service Configuration

Check that the fulfillment service is configured with the correct issuer URL:

```bash
kubectl get deployment fulfillment-service -n osac -o yaml | grep issuerUrl
```

### 4. Test Authentication

#### Test with `osac login` (Recommended)

The `osac` CLI provides a `login` command that handles authentication automatically:

```bash
# Login using the device flow (default — opens browser for authentication)
osac login https://fulfillment-api.osac.svc.cluster.local:8000

# Login using password flow (non-interactive)
osac login https://fulfillment-api.osac.svc.cluster.local:8000 \
  --user USERNAME --password PASSWORD

# Login using client credentials flow (for service accounts)
osac login https://fulfillment-api.osac.svc.cluster.local:8000 \
  --client-id my-service --client-secret MY_SECRET

# Login with a custom CA certificate
osac login https://fulfillment-api.osac.svc.cluster.local:8000 \
  --ca-file /path/to/ca.crt

# Login with a token script (for Kubernetes service account tokens)
osac login https://fulfillment-api.osac.svc.cluster.local:8000 \
  --token-script 'kubectl create token -n osac client --duration 1h'
```

After login, the configuration is saved to `~/.config/osac/` and subsequent `osac` commands use the
stored credentials automatically.

```bash
# Verify login works
osac get cluster
```

#### Test with a Keycloak JWT Token (Manual)

1. **Get a token from Keycloak** (using the osac-cli client):

   ```bash
   TOKEN=$(curl -k -X POST \
     https://keycloak.keycloak.svc.cluster.local:8000/realms/osac/protocol/openid-connect/token \
     -d "client_id=osac-cli" \
     -d "username=USERNAME" \
     -d "password=PASSWORD" \
     -d "grant_type=password" \
     -d "scope=openid" | jq -r '.access_token')
   ```

2. **Test the API with the token**:

   ```bash
   SERVICE_URL=$(kubectl get route -n osac fulfillment-api -o jsonpath='{.spec.host}')

   curl -k -H "Authorization: Bearer ${TOKEN}" \
     https://${SERVICE_URL}/api/fulfillment/v1/cluster_templates
   ```

### 5. Verify Authorization

Test that authorization rules are working:

1. **Test as a client user** (should have limited access):
   - Login with a regular Keycloak user: `osac login <address> --user alice --password ...`
   - Verify access to allowed methods: `osac get cluster`, `osac get computeinstance`
   - Verify denial of admin-only methods (e.g., hub operations)

2. **Test as a tenant admin** (should have client + user management access):
   - Login with a user that has the `tenant-admin` realm role
   - Verify access to user management: `osac get user`

3. **Test as an admin** (should have full access):
   - Login with an admin service account using client credentials flow
   - Verify access to all methods including private APIs

## Troubleshooting

### Keycloak Pod Not Starting

- Check pod logs: `kubectl logs -n keycloak <pod-name>`
- Verify database connectivity
- Check certificate configuration

### Token Validation Failing

- Verify the issuer URL matches exactly (including protocol, hostname, port, and path)
- Check that Keycloak is accessible from the fulfillment service pods
- Verify the realm name is correct (`osac`)
- Check network policies if using them

### Authorization Denied

- Verify the user has the correct authentication method
- Check the authorization interceptor logs
- Verify the subject name mapping in the authorization policy

### Certificate Issues

- Ensure cert-manager is installed and working
- Verify the issuer is correctly configured
- Check certificate status: `kubectl get certificates -n keycloak`

## Additional Resources

- [Keycloak Documentation](https://www.keycloak.org/documentation)
- [Open Policy Agent (OPA) Documentation](https://www.openpolicyagent.org/docs/latest/)
- [Helm Chart README](it/charts/keycloak/README.md)
- [Service Chart README](charts/service/README.md)
