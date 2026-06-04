# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Overview

The fulfillment-service is a gRPC server with REST gateway for managing infrastructure resources
(clusters, hosts, compute instances, networking). It uses PostgreSQL for storage, OPA for
authorization, and supports Kubernetes deployment via Helm.

## Build and Test Commands

```bash
# Build binaries
go build ./cmd/fulfillment-service
go build ./cmd/osac

# Run unit tests only (excludes integration tests in it/)
ginkgo run -r internal

# Run a specific package's tests
ginkgo run internal/servers

# Run tests matching a name pattern
ginkgo run -r internal --focus="CreateCluster"

# Run tests with verbose output
ginkgo run -v internal/servers

# Skip tests matching a pattern
ginkgo run -r internal --skip="database"

# Proto: lint and generate
buf lint
buf generate

# Run all tests including integration (requires kind cluster)
ginkgo run -r
```

### Integration Tests

```bash
# Run integration tests (creates a kind cluster)
ginkgo run it

# Preserve cluster for debugging
IT_KEEP_KIND=true ginkgo run it

# Run only setup (create cluster without tests)
IT_KEEP_KIND=true ginkgo run --label-filter=setup it

# Clean up preserved cluster
kind delete cluster --name fulfillment-service-it
```

Requires `/etc/hosts` entries:
- `127.0.0.1 keycloak.keycloak.svc.cluster.local`
- `127.0.0.1 fulfillment-api.osac.svc.cluster.local`
- `127.0.0.1 fulfillment-internal-api.osac.svc.cluster.local`

### Running Locally

See [README.md](README.md) for instructions on running the service locally, including PostgreSQL
setup and starting the gRPC server and REST gateway.

## Architecture

### Code Organization

- `cmd/fulfillment-service/` - Service binary entry point (calls `internal/cmd/service.Root()`)
- `cmd/osac/` - CLI binary entry point (calls `internal/cmd/cli.Root()`)
- `internal/cmd/service/start/` - Server startup commands (grpcserver, restgateway, controller)
- `internal/servers/` - gRPC service implementations (one `*_server.go` per resource)
- `internal/database/` - PostgreSQL access layer with generic DAO
- `internal/database/dao/` - Generic type-safe DAO (`GenericDAO[O Object]`)
- `internal/database/migrations/` - SQL migration files
- `internal/api/` - Generated Go code from protobuf (do not edit)
- `internal/auth/` - Authentication, tenancy, and attribution logic
- `internal/controllers/` - Kubernetes controllers
- `internal/testing/` - Test utilities (test server, database helpers, kind helpers)
- `proto/` - Protocol Buffer definitions
- `it/` - Integration tests
- `charts/` - Helm charts

### Proto Structure

Protos are split into public and private APIs under `proto/`:

```text
proto/public/osac/public/v1/   - User-facing API (read-heavy, limited write)
proto/private/osac/private/v1/ - Admin/controller API (full CRUD + Signal RPC)
proto/tests/osac/tests/v1/     - Test-only proto definitions
```

Each resource has `<resource>_type.proto` (message definitions) and `<resource>s_service.proto` (RPC
methods). Generated Go code lands in `internal/api/osac/{public,private}/v1/`.

### Server Implementation Pattern

Public servers delegate to private servers and add tenant/auth logic:
- `ClustersServer` (public) wraps `PrivateClustersServer` (private)
- Builder pattern: `ClustersServerBuilder` configures dependencies
- Both server files live in `internal/servers/` (e.g., `clusters_server.go` + `private_clusters_server.go`)

### Database Layer

Uses `pgx/v5` with a generic DAO pattern:
- `GenericDAO[O Object]` provides type-safe CRUD for any protobuf message
- Resources stored as JSON-serialized protobuf in a `data` column
- Standard columns: `id`, `name`, `creation_timestamp`, `deletion_timestamp`, `finalizers`, `creator`, `tenant`, `labels`, `annotations`, `data`
- CEL filter expressions translated to SQL WHERE clauses via `FilterTranslator`
- Migrations in `internal/database/migrations/` (numbered `*.up.sql` files)

### gRPC Interceptor Chain

The gRPC server uses chained interceptors (configured in `internal/cmd/service/start/grpcserver/`):
1. Panic recovery
2. Prometheus metrics
3. Structured logging (slog)
4. Authentication (JWT validation)
5. Database transaction management

### Mock Generation

Uses `go.uber.org/mock` (uber-go/mock). Mocks are generated with `//go:generate mockgen` directives
and live alongside source files (e.g., `attribution_logic_mock.go`).

### Testing Pattern

Tests use Ginkgo v2 + Gomega. Typical suite setup in `*_suite_test.go`:
- `BeforeSuite` initializes logger, auth logic, database
- `DeferCleanup` for teardown
- `dao.CreateTables[T]()` dynamically creates test schemas

## Automated Hooks

The following automated checks are configured and should be run at the appropriate times:

- **After proto changes**: When a `.proto` file is edited, run `buf lint && buf generate` to keep
  generated Go code in sync.
- **After Go module changes**: When `go.mod` is edited, run `go mod tidy`.
- **Before committing**: Run `buf lint` before every `git commit` to catch proto issues early.
- **Before creating a PR**: Run `gofmt -s -w .` (auto-formats, then fails if any files changed — commit the fixes first), `buf lint`, and `ginkgo run -r internal`.

## CLI Command Help Text

When adding or modifying CLI commands, write help text (both `Short` and `Long` descriptions, as
well as flag help strings) using Markdown. The help system renders Markdown at display time, so the
source strings should use Markdown syntax for emphasis, inline code, code blocks, and similar
formatting.

Because raw backticks would conflict with Go string syntax, use the `{{ bt }}` template function for
inline code and `{{ bt 3 }}` for fenced code blocks.

For flag help, start with a short type hint in italics (e.g. `_[BOOLEAN]_`, `_URL_`,
`_FILE|DIRECTORY_`) followed by a dash and the description.

Refer to existing commands such as `internal/cmd/cli/login/login_cmd.go` for style and examples of
how help text is structured.

## API Design Guidelines

### Annotations vs Spec/Status Fields

Annotations (`metadata.annotations`) are intended for arbitrary, user-controlled metadata. They are
not part of the system's data model and must not be used to represent relationships, configuration,
or state that the system depends on. When a resource has a relationship to a parent or to another
resource, or when the system needs to store operational data, use a field in `spec` (for
user-provided, declarative configuration) or `status` (for system-managed state) instead.

For example, if a Subnet belongs to a VirtualNetwork, the correct approach is a
`spec.virtual_network` field that holds the parent ID, not an annotation like
`osac.io/owner-reference`. Spec fields are typed, validated, documented in the schema, and visible
in generated clients, whereas annotations are opaque strings with no schema enforcement.

In summary:

- **Spec fields** are for user-declared desired state, including references to parent or related
  resources.
- **Status fields** are for system-managed observed state, including identifiers the system assigns
  (e.g., which hub was selected).
- **Annotations** are for users to attach their own unstructured metadata. The system should never
  read annotations to make decisions.

Some existing proto files document parent relationships via annotations. That is legacy guidance and
should not be followed when designing new resources or refactoring existing ones.

## Common Pitfalls

- Proto changes require both `buf lint` and `buf generate` before committing
- `SERVICE_SUFFIX` lint rule is intentionally excluded in `buf.yaml`
- Unit tests: run `ginkgo run -r internal` (not `ginkgo run -r`) to avoid triggering integration tests
- The `internal/api/` directory is fully generated - never edit files there manually
