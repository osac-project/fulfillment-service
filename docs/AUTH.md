# Keycloak Setup and Configuration Guide

This guide explains how to set up Keycloak as an Identity Provider (IDP) for the fulfillment service and configure the necessary mappings and authorization rules.

> **Note**: While this guide focuses on Keycloak (including deployment steps using the provided Helm chart), the fulfillment service is designed to work with **any OAuth-compatible Identity Provider**. The service only requires:
> - A valid OAuth issuer URL
> - JWT tokens containing `username` and `groups` claims (or configurable alternative claims)
> - The ability to validate tokens using the issuer's public keys
>
> If you're using a different OAuth IDP (such as Okta, Auth0, Azure AD, Google Identity, etc.), you can skip the Keycloak installation sections and proceed directly to the [Fulfillment Service Configuration](#fulfillment-service-configuration) section, adapting the configuration steps to your IDP's specific requirements.

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

## Prerequisites

Before installing Keycloak, ensure you have:

- A Kubernetes cluster (Kind or OpenShift)
- [cert-manager](https://cert-manager.io/) operator installed
- At least one cert-manager issuer configured (ClusterIssuer or Issuer)
- [Authorino](https://github.com/Kuadrant/authorino) operator installed (for the fulfillment service)

## Installing Keycloak

The fulfillment service includes a Helm chart for deploying Keycloak. The chart is published with each release as an OCI image at [ghcr.io/osac/charts/keycloak](https://github.com/osac-project/fulfillment-service/pkgs/container/charts%2Fkeycloak).

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

Replace `0.0.27` with the version you want to use. You can find available versions at the [chart registry](https://github.com/osac-project/fulfillment-service/pkgs/container/charts%2Fkeycloak).

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
| `images.postgres` | The PostgreSQL container image | No | `quay.io/sclorg/postgresql-15-c9s:latest` |

### Important Notes

- The `hostname` parameter is critical and must match the hostname that Keycloak will use to reference itself, meaning that when Keycloak redirects users, or generates an URL for itself, it will use this host name. This is also used for token issuer URLs.
- The default admin credentials are:
  - Username: `admin`
  - Password: `admin`
  - **Important**: Change these credentials in production environments!

## Keycloak Configuration

The Helm chart includes a pre-configured realm named `osac` with the following setup:

### Realm Configuration

- **Realm Name**: `osac`

### Pre-configured Clients

The realm includes the **fulfillment-cli** (Public) client:
   - Client ID: `fulfillment-cli`
   - Type: Public client (no client secret required)
   - Enabled flows: Standard flow (authorization code)
   - Use case: Command-line interface authentication

### Accessing Keycloak Admin Console

To access the Keycloak Admin Console:

1. **Access the console**:

   Add the host name used internally in the cluster pointing to `127.0.0.1` to `/etc/hosts`:

   ```
   127.0.0.1 keycloak.keycloak.svc.cluster.local
   ```

   Open your browser and navigate to https://keycloak.keycloak.svc.cluster.local:8000, then
   accept the self-signed certificate warning.

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
   pod=$(kubectl get pods -n keycloak -l app=keycloak-service -o json | jq -r '.items[].metadata.name')
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
   cp realm.json charts/keycloak/files/realm.json
   ```

## Fulfillment Service Configuration

After Keycloak is installed, you need to configure the fulfillment service to use Keycloak as its identity provider.

### 1. Configure the Issuer URL

The fulfillment service needs to know the Keycloak issuer URL to validate JWT tokens. The issuer URL format is:

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

### 3. Update the AuthConfig Resource

The fulfillment service uses Authorino for authentication and authorization. The `AuthConfig` resource must be configured with the Keycloak issuer URL.

The AuthConfig is automatically generated by the Helm chart from the template at `charts/service/templates/service/authconfig.yaml`. The configuration includes:

#### Authentication Methods

1. **Kubernetes Service Account Authentication** (`fulfillment-api`)
   - Validates Kubernetes service account tokens
   - Used for internal service-to-service communication

2. **Keycloak JWT Authentication** (`keycloak-jwt`)
   - Validates JWT tokens issued by Keycloak
   - Uses the issuer URL to fetch the public keys for token validation

Example AuthConfig snippet:

```yaml
authentication:
  "keycloak-jwt":
    jwt:
      issuerUrl: https://keycloak.keycloak.svc.cluster.local:8000/realms/osac
    overrides:
      authnMethod:
        value: jwt
```

### 4. Update the Server Configuration

The fulfillment service server component also needs to be configured with the trusted token issuer. This is done via the `--grpc-authn-trusted-token-issuers` flag in the deployment.

The Helm chart automatically sets this from the `auth.issuerUrl` value. In the deployment, you'll see:

```yaml
- --grpc-authn-trusted-token-issuers=https://keycloak.keycloak.svc.cluster.local:8000/realms/osac
```

### 5. Trusted vs Advertised Token Issuers

The fulfillment service maintains two separate lists of token issuers:

1. **Trusted Token Issuers**: These are the advertised trusted issuers. This list is configured in:
   - Authorino AuthConfig (for HTTP/gRPC gateway authentication)
   - Server command-line flag `--grpc-authn-trusted-token-issuers` (for direct gRPC authentication)

2. **Advertised Token Issuers**: These are the issuers that the service advertises to clients (primarily for CLI usage). This allows clients to discover which issuers they can use without explicitly specifying them. The advertised issuers are returned via the metadata API and may or may not be the same as the trusted issuers.

   The advertised issuers are configured via the `--grpc-authn-trusted-token-issuers` flag

## User and Group Mapping

The fulfillment service maps users and groups from Keycloak (or any OAuth IDP) to its internal user and tenant concepts. This mapping is configured in the Authorino AuthConfig resource.

### Key Concepts

- **Users**: Represent individual authenticated entities (users or service accounts)
- **Tenants**: Represent groups of users. In Keycloak, these map to **groups**, but the fulfillment service refers to them as **tenants**
- **Organizations**: The fulfillment service does **not** have an explicit "organization" concept. Organizations and tenants are defined and managed in the external identity provider (Keycloak)

### Claim Extraction Configuration

The extraction of user identity and groups from JWT token claims is configured in the `AuthConfig` resource's `response` section. The current configuration extracts:

- **Username**: From the `username` claim in the JWT token
- **Groups**: From the `groups` claim in the JWT token (these become tenants in the fulfillment service)

The configuration is located in `charts/service/templates/service/authconfig.yaml`:

```yaml
response:
  success:
    headers:
      "x-subject":
        json:
          properties:
            source:
              expression: |
                auth.identity.authnMethod
            user:
              expression: |
                auth.identity.authnMethod == "serviceaccount"? auth.identity.user.username: auth.identity.username
            groups:
              expression: |
                auth.identity.authnMethod == "serviceaccount"? auth.identity.user.groups: auth.identity.groups
```

### Customizing Claim Extraction

You can modify the claim extraction to use different claims or sources. For example:

- To use a different claim for username (e.g., `preferred_username` or `email`):
  ```yaml
  user:
    expression: |
      auth.identity.authnMethod == "serviceaccount"? auth.identity.user.username: auth.identity.claims.preferred_username
  ```

- To use a different claim for groups (e.g., `realm_access.roles`):
  ```yaml
  groups:
    expression: |
      auth.identity.authnMethod == "serviceaccount"? auth.identity.user.groups: auth.identity.claims.realm_access.roles
  ```

### Configuring Keycloak to Include Groups in Tokens

To ensure that user groups are included in the JWT tokens issued by Keycloak:

1. **Access the Keycloak Admin Console** (see [Accessing Keycloak Admin Console](#accessing-keycloak-admin-console))

2. **Navigate to the Client**:
   - Go to **Clients** → Select your client (e.g., `fulfillment-cli`)

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
     - **Full group path**: `false` (or `true` if you want full paths like `/tenant-a/team-1`)
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

2. **Default Tenants**: The tenants that will be automatically assigned to a resource when the user
   creates it without explicitly specifying tenants. The default tenants are always a subset of the
   assignable tenants.

3. **Visible Tenants**: The tenants from which a user can see resources. When listing or querying
   resources, only those belonging to visible tenants will be returned. Users can only explicitly
   assign tenants that are both assignable and visible to them. Some default tenants may be
   invisible, meaning a user could have resources automatically assigned to tenants they cannot
   later query.

The tenancy logic can be configured using the `--tenancy-logic` command-line flag when starting the
fulfillment service.

### Additional Tenancy Concepts

1. **Shared Tenant**: The `shared` tenant is a special tenant that is always included in the
   visible tenants for all users. Resources assigned to the `shared` tenant are visible to
   **everyone**. This is useful for templates, shared configurations, or other resources that
   should be accessible across all tenants.

2. **Multi-Tenant Users**: A user can belong to multiple tenants. This is configured in Keycloak by
   assigning the user to multiple groups. The fulfillment service will reflect this one-to-many
   mapping in both the assignable and visible tenant sets.

3. **Tenant Assignment**: When a user creates a resource:

   - A user assignment is recorded in the `metadata.creators` field of the object, and is purely
     informative. The system doesn't currently use it to make any authorization or visibility
     decisions.
   - If the user explicitly specifies tenants, those tenants are assigned. The user can only
     explicitly assign tenants that are both assignable and visible.
   - If the user doesn't specify tenants, the default tenants are automatically assigned. Note that
     some default tenants may be invisible to the user.
   - Tenant assignment is recorded in the `metadata.tenants` field and is used by the server to
     make visibility decisions.

4. **Tenant Visibility**: When a user queries resources, the visible tenants determine what they
   can see. A user can only see a resource if the intersection between the user's visible tenants
   and the resource's assigned tenants is not empty. Since both users and resources can have
   multiple tenants, a non-empty intersection is sufficient for visibility.

### Tenancy Logic Implementations

The following tenancy logic implementations are available:

#### Default (JWT-Authenticated Users)

Use `--tenancy-logic=default` for JWT-authenticated users (Keycloak):

- **Assignable Tenants**: All groups from the user's JWT token (`groups` claim)
- **Default Tenants**: Same as assignable tenants (all user's groups)
- **Visible Tenants**: All user's groups plus the `shared` tenant

Example:
- User `alice` belongs to groups: `["team-a", "team-b"]`
- Assignable tenants: `["team-a", "team-b"]`
- Default tenants: `["team-a", "team-b"]`
- When `alice` creates a cluster without specifying tenants:
  - The cluster is assigned to tenants: `["team-a", "team-b"]`
- When `alice` lists clusters:
  - She can see clusters from: `["team-a", "team-b", "shared"]`

#### Service Account

Use `--tenancy-logic=serviceaccount` for service account authentication:

- **Assignable Tenants**: The service account's namespace
- **Default Tenants**: Same as assignable (the namespace)
- **Visible Tenants**: The namespace plus the `shared` tenant

Example:
- Service account `system:serviceaccount:osac:controller`
- Assignable tenants: `["osac"]`
- Default tenants: `["osac"]`
- When creating a resource without specifying tenants:
  - Assigned to tenant: `["osac"]`
- When listing resources:
  - Can see resources from: `["osac", "shared"]`

#### Guest

Use `--tenancy-logic=guest` for guest user access:

- **Assignable Tenants**: The `guest` tenant only
- **Default Tenants**: The `guest` tenant
- **Visible Tenants**: The `guest` tenant plus the `shared` tenant

This is intended only for development and testing environments, in combination with the `guest`
authentication function.

Example:
- Any user (authenticated or guest)
- Assignable tenants: `["guest"]`
- Default tenants: `["guest"]`
- When creating a resource without specifying tenants:
  - Assigned to tenant: `["guest"]`
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

3. **Configure Group Mapper** (as described in [Configuring Keycloak to Include Groups in Tokens](#configuring-keycloak-to-include-groups-in-tokens))

### Future Enhancements

The tenancy logic is now configurable via the `--tenancy-logic` flag. Future enhancements may include:
- Support for an additional "organization" layer (requiring development)
- Custom tenant naming conventions
- Additional tenancy logic implementations for specific use cases

## Authorization Configuration

The fulfillment service uses Open Policy Agent (OPA) Rego policies for authorization. The authorization rules are defined in the `AuthConfig` resource.
The defined rules are a very simple set of intended for development and testing purposes. Further rules and policies can be configured according to the different needs.

### Authorization Rules Overview

The authorization policy distinguishes between two types of subjects:

1. **Admin Subjects**: Service accounts with administrative privileges
   - `system:serviceaccount:<namespace>:admin`
   - `system:serviceaccount:<namespace>:controller`

2. **Client Subjects**: All other authenticated users (JWT tokens from Keycloak or other service accounts)

### Authorization Logic

The authorization policy allows:

1. **Everyone** (authenticated users):
   - Metadata endpoints (`/metadata.*`)
   - gRPC reflection endpoints (`/grpc.reflection.*`)
   - Health check endpoints (`/grpc.health.*`)

2. **Client Users** (non-admin):
   - Specific gRPC methods for:
     - Events: `Watch`
     - Cluster Templates: `Get`, `List`
     - Clusters: `Create`, `Delete`, `Get`, `GetKubeconfig`, `GetKubeconfigViaHttp`, `GetPassword`, `GetPasswordViaHttp`, `List`, `Update`
     - Host Classes: `Get`, `List`
     - Host Pools: `Create`, `Delete`, `Get`, `List`, `Update`
     - Hosts: `Create`, `Delete`, `Get`, `List`, `Update`
     - Compute Instance Templates: `Get`, `List`
     - Compute Instances: `Create`, `Delete`, `Get`, `List`, `Update`

3. **Admin Users**:
   - All methods (full access)

### Customizing Authorization Rules

To modify authorization rules, edit the `authorization` section in the AuthConfig. The Rego policy is located in:

- Template: `charts/service/templates/service/authconfig.yaml`
- Base manifest: `manifests/base/service/authconfig.yaml`

Example: To add a new allowed method for client users, add it to the list in the `is_client` rule:

```rego
allow {
  is_client
  grpc_method in {
    # ... existing methods ...
    "/fulfillment.v1.NewService/NewMethod",
  }
}
```

Example: To add a new admin subject, add it to the `admin_subjects` set:

```rego
admin_subjects := {
  "system:serviceaccount:osac:admin",
  "system:serviceaccount:osac:controller",
  "system:serviceaccount:osac:new-admin",  # New admin
}
```

After modifying the AuthConfig, apply the changes:

```bash
kubectl apply -f manifests/base/service/authconfig.yaml
```

Or if using Helm:

```bash
helm upgrade fulfillment-service charts/service -n osac
```

## Authorization Flow

The fulfillment service uses a two-level authorization approach:

1. **Authorino (External Authorization)**: Validates the operation type (not the specific resource)
2. **Fulfillment Service (Internal Authorization)**: Validates access to specific resources based on tenancy

### Authorization Flow Diagram

```
┌─────────────┐
│   User      │
│  (Client)   │
└──────┬──────┘
       │
       │ 1. User logs in via OAuth IDP (Keycloak)
       │    Gets back JWT token
       │
       ▼
┌─────────────┐
│   User      │
│  (with JWT) │
└──────┬──────┘
       │
       │ 2. User attempts CRUD operation
       │    (e.g., Create Cluster, List Templates)
       │
       ▼
┌─────────────┐
│   Envoy     │
│   Gateway   │
└──────┬──────┘
       │
       │ 3. Envoy forwards request details to Authorino
       │
       ▼
┌─────────────┐
│  Authorino  │
│             │
│  - Validates JWT token
│  - Extracts user and groups
│  - Evaluates Rego policy
│  - Checks if operation is allowed
│    (based on operation type, not resource)
└──────┬──────┘
       │
       ├─── 4a. Authorized? ──► Continue
       │
       └─── 4b. Denied? ──► Return 403 Forbidden
                            ┌─────────────┐
                            │   User      │
                            │  (Error)    │
                            └─────────────┘
       │
       ▼ (if authorized)
┌─────────────────────┐
│ Fulfillment Service │
│                     │
│  - Validates user can access
│    the specific resource
│  - Applies tenancy filtering
│  - Performs the operation
│    or returns error
└──────┬──────────────┘
       │
       ├─── 5a. Success ──► Return result
       │                    ┌─────────────┐
       │                    │   User      │
       │                    │  (Result)   │
       │                    └─────────────┘
       │
       └─── 5b. Error ──► Return error
                            ┌─────────────┐
                            │   User      │
                            │  (Error)    │
                            └─────────────┘
```

### Step-by-Step Authorization Process

1. **User Authentication**:
   - User logs in through Keycloak (OAuth IDP)
   - Receives a JWT access token containing:
     - Username (`username` claim)
     - Groups (`groups` claim) - these become tenants
     - Roles (if configured)

2. **Request Initiation**:
   - User makes a request to the fulfillment service API
   - Includes the JWT token in the `Authorization: Bearer <token>` header
   - Request goes through Envoy gateway

3. **Authorino Validation**:
   - Envoy forwards the request to Authorino
   - Authorino:
     - Validates the JWT token signature and expiration
     - Extracts user identity and groups from token claims
     - Evaluates the Rego authorization policy
     - Checks if the **operation type** is allowed for this user
     - Does **NOT** check access to specific resources

4. **Authorino Decision**:
   - **If authorized**: Request proceeds to fulfillment service
   - **If denied**: Returns `403 Forbidden` to the user

5. **Fulfillment Service Validation**:
   - If Authorino approved, the request reaches the fulfillment service
   - The service:
     - Extracts user and tenant information from the `x-subject` header (set by Authorino)
     - Applies tenancy logic to determine:
       - **Assignable tenants**: Which tenants can be assigned to resources
       - **Default tenants**: Which tenants to assign if not explicitly specified
       - **Visible tenants**: Which tenants the user can query resources from
     - Validates access to specific resources
     - Performs the operation or returns an error

### Example Authorization Scenarios

#### Scenario 1: Client User Creating a Cluster

1. User `alice` (belongs to groups: `["team-a"]`) authenticates with Keycloak
2. `alice` sends: `POST /api/fulfillment/v1/clusters` with JWT token
3. **Authorino checks**:
   - Is `alice` authenticated? ✅ Yes
   - Is `Create` operation allowed for client users? ✅ Yes (in the allowed methods list)
   - **Result**: Authorized ✅
4. **Fulfillment Service**:
   - Extracts user: `alice`, groups: `["team-a"]`
   - Determines assignable tenants: `["team-a"]`, default tenants: `["team-a"]`, visible tenants: `["team-a", "shared"]`
   - No tenants specified, so assigns the default tenants: `["team-a"]`
   - **Result**: Cluster created ✅

#### Scenario 2: Client User Accessing Admin-Only Method

1. User `alice` sends: `POST /api/fulfillment/v1/admin-only-method` with JWT token
2. **Authorino checks**:
   - Is `alice` authenticated? ✅ Yes
   - Is `admin-only-method` allowed for client users? ❌ No (not in allowed methods list)
   - Is `alice` an admin? ❌ No
   - **Result**: Denied ❌
3. **User receives**: `403 Forbidden` (never reaches fulfillment service)

#### Scenario 3: Admin User Accessing Any Method

1. Service account `system:serviceaccount:osac:admin` sends request with service account token
2. **Authorino checks**:
   - Is service account authenticated? ✅ Yes
   - Is the subject in `admin_subjects`? ✅ Yes
   - **Result**: Authorized ✅ (admins can access everything)
3. **Fulfillment Service**:
   - Processes the request with full access
   - **Result**: Operation succeeds ✅

#### Scenario 4: User Viewing Resources (Tenancy Filtering)

1. User `alice` (belongs to groups: `["team-a"]`) sends: `GET /api/fulfillment/v1/clusters`
2. **Authorino checks**:
   - Is `List` operation allowed? ✅ Yes
   - **Result**: Authorized ✅
3. **Fulfillment Service**:
   - Determines visible tenants: `["team-a", "shared"]`
   - Queries database filtering to only return clusters from these tenants
   - **Result**: Returns only clusters from `team-a` and `shared` tenants ✅

### Roles and Permissions

The fulfillment service does not have an explicit "role" concept, but it distinguishes between:

1. **Admin Users**:
   - Defined by service account names in the Rego policy
   - Currently: `system:serviceaccount:<namespace>:admin` and `system:serviceaccount:<namespace>:controller`
   - Have full access to all operations

2. **Client Users**:
   - All other authenticated users (JWT tokens from Keycloak or other service accounts)
   - Have access to a specific list of operations (defined in the Rego policy)
   - Access is further restricted by tenancy (can only see resources from their tenants)

### Applying Roles in Keycloak

While the fulfillment service doesn't have explicit roles, you can use Keycloak roles for:

1. **Keycloak-Level Authorization**: Use Keycloak roles to control who can authenticate or which clients they can use
2. **Future Integration**: If role-based authorization is added to the fulfillment service, Keycloak roles can be included in tokens and used in the Rego policy

To add roles in Keycloak:

1. **Create Realm Roles**:
   - Go to **Realm roles** → **Create role**
   - Examples: `tenant-admin`, `tenant-user`, `viewer`, `editor`

2. **Assign Roles to Users**:
   - Go to **Users** → Select user → **Role mapping** tab
   - Assign realm roles to the user

3. **Include Roles in Tokens** (if needed for future use):
   - Configure a **Realm Roles** mapper in the client scope
   - Set **Token Claim Name**: `roles`
   - Enable **Add to access token`

## Verification

### 1. Verify Keycloak is Running

```bash
kubectl get pods -n keycloak
kubectl get svc -n keycloak
```

### 2. Verify Keycloak Realm

Access the Keycloak Admin Console and verify:
- The `osac` realm exists
- The `fulfillment-cli` and `fulfillment-controller` clients are configured
- The realm is enabled

### 3. Verify Fulfillment Service Configuration

Check that the fulfillment service is configured with the correct issuer URL:

```bash
kubectl get deployment fulfillment-service -n osac -o yaml | grep issuerUrl
kubectl get authconfig fulfillment-service -n osac -o yaml | grep issuerUrl
```

### 4. Test Authentication

#### Test with a Keycloak JWT Token

1. **Get a token from Keycloak** (using the fulfillment-cli client):

   ```bash
   # Get token (replace USERNAME and PASSWORD with actual credentials)
   TOKEN=$(curl -k -X POST \
     https://localhost:8443/realms/osac/protocol/openid-connect/token \
     -d "client_id=fulfillment-cli" \
     -d "username=USERNAME" \
     -d "password=PASSWORD" \
     -d "grant_type=password" \
     -d "scope=openid" | jq -r '.access_token')
   ```

2. **Test the API with the token**:

   ```bash
   # Get the service URL
   SERVICE_URL=$(kubectl get route -n osac fulfillment-api -o jsonpath='{.spec.host}')

   # Make a request
   curl -k -H "Authorization: Bearer ${TOKEN}" \
     https://${SERVICE_URL}/api/fulfillment/v1/cluster_templates
   ```

#### Test with Kubernetes Service Account Token

```bash
# Get a service account token
TOKEN=$(kubectl create token -n osac client)

# Test the API
curl -k -H "Authorization: Bearer ${TOKEN}" \
  https://${SERVICE_URL}/api/fulfillment/v1/cluster_templates
```

### 5. Verify Authorization

Test that authorization rules are working:

1. **Test as a client user** (should have limited access):
   - Use a Keycloak JWT token from a regular user
   - Verify access to allowed methods
   - Verify denial of admin-only methods

2. **Test as an admin** (should have full access):
   - Use a service account token from the `admin` or `controller` service account
   - Verify access to all methods

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
- Check the Rego policy logs in Authorino
- Verify the subject name mapping in the authorization policy

### Certificate Issues

- Ensure cert-manager is installed and working
- Verify the issuer is correctly configured
- Check certificate status: `kubectl get certificates -n keycloak`

## Additional Resources

- [Keycloak Documentation](https://www.keycloak.org/documentation)
- [Authorino Documentation](https://github.com/Kuadrant/authorino)
- [Open Policy Agent (OPA) Documentation](https://www.openpolicyagent.org/docs/latest/)
- [Helm Chart README](charts/keycloak/README.md)
- [Service Chart README](charts/service/README.md)
