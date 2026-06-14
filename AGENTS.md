# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Overview

The fulfillment-service is a gRPC server with REST gateway for managing infrastructure resources (clusters, hosts, compute instances, networking). It uses PostgreSQL for storage, OPA for authorization, and supports Kubernetes deployment via Helm/Kustomize.

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

# Lint
uv run dev.py lint

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

# Use kustomize instead of default Helm deployment
IT_DEPLOY_MODE=kustomize ginkgo run it

# Clean up preserved cluster
kind delete cluster --name fulfillment-service-it
```

Requires `/etc/hosts` entries:
- `127.0.0.1 keycloak.keycloak.svc.cluster.local`
- `127.0.0.1 fulfillment-api.osac.svc.cluster.local`
- `127.0.0.1 fulfillment-internal-api.osac.svc.cluster.local`

### Running Locally

See [README.md](README.md) for instructions on running the service locally, including PostgreSQL setup and starting the gRPC server and REST gateway.

## Development Tooling

Development and build tasks are automated through the `dev.py` script, which is run with `uv run
dev.py <command>`. When a new task needs to be automated (for example building, formatting,
generating code, running tests with specific options, or installing a tool), it should be added as a
new command or sub-command in the `dev/` Python package rather than as a standalone shell script or
a Makefile target.

The `dev/` package is organized as follows:

- `dev.py` - Entry point that registers all commands via Click.
- `dev/commands.py` - Helpers for running external commands, with automatic resolution of binaries
  installed in the project's `bin/` directory.
- `dev/dirs.py` - Functions that return well-known project directories (`project()`, `bin()`).
- `dev/tools.py` - Definitions of external tools (name, version, checksums) used by the `setup`
  command.
- `dev/setup.py` - The `setup` command, which downloads and verifies tool binaries from their
  GitHub release pages.
- `dev/lint.py` - The `lint` command.

To add a new command, create a new module in `dev/` (e.g. `dev/build.py`), define a Click command
in it, register it in `dev/__init__.py`, and add it to the CLI group in `dev.py`. Follow the
existing modules as examples.

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

Each resource has `<resource>_type.proto` (message definitions) and `<resource>s_service.proto` (RPC methods). Generated Go code lands in `internal/api/osac/{public,private}/v1/`.

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

### Introducing New Object Types

When a migration needs to introduce a new kind of object, use the `create_object_schema` stored
procedure instead of writing the boilerplate DDL by hand. The procedure receives the plural table
name and creates the object table with all standard DAO columns, the corresponding archive table,
the standard indexes on `name`, `creator`, `tenant`, and `labels`, and the tenant foreign key
constraint referencing the `organizations` table. For example:

```sql
call create_object_schema('widgets');
```

This single call is equivalent to manually creating the `widgets` and `archived_widgets` tables, the
four indexes (`widgets_by_name`, `widgets_by_creator`, `widgets_by_tenant`, `widgets_by_label`), and
the `widgets_tenant_fk` foreign key. Any resource-specific extras such as additional indexes,
immutability triggers, or materialized helper tables should be added in the same migration after the
procedure call.

### Enforcing Cross-Object Constraints

Because objects are stored as JSON-serialized protobuf in a single `data` column, the database
cannot natively enforce uniqueness or referential integrity for values buried inside the JSON. When
a constraint must span multiple rows or multiple object types (for example, ensuring that an e-mail
address is claimed by at most one user), use the **materialized helper table** pattern:

1. Create a small helper table whose schema mirrors the relationship or constraint (e.g., a
   `user_emails` table with `email` as the primary key and `username` as a foreign key).
2. Add an index on columns used for lookup in the trigger (e.g., the `username` column) so that the
   trigger's delete-and-reinsert cycle remains efficient.
3. Write a PL/pgSQL trigger function that fires after insert or update on the object table. The
   function deletes stale rows for the affected object, then re-inserts the current values
   extracted from the JSONB `data` column. Catch constraint violations and re-raise them with a
   descriptive application-level error code.
4. Attach the trigger to the object table.

This keeps the source of truth in the JSONB `data` column while letting PostgreSQL enforce the
constraint through the helper table's primary key or unique index. A typical migration looks like
this:

```sql
-- Helper table that mirrors the constraint:
create table user_emails (
  email text not null primary key,
  username text not null references users(id) on delete cascade
);
create index user_emails_by_username on user_emails (username);

-- Trigger function that materializes values from the JSONB data column:
create function materialize_user_emails() returns trigger as $$
declare
  e text;
begin
  delete from user_emails where username = new.id;

  for e in select jsonb_array_elements_text(new.data->'spec'->'emails')
  loop
    begin
      insert into user_emails (email, username) values (e, new.id);
    exception when unique_violation then
      raise exception using
        errcode = 'Z0002',
        message = format('email ''%s'' is already used by another user', e);
    end;
  end loop;

  return new;
end;
$$ language plpgsql;

create trigger materialize_user_emails
  after insert or update on users
  for each row
  execute function materialize_user_emails();
```

Because the trigger fires only on `insert` or `update`, rows that already exist when the migration
runs will not have corresponding entries in the helper table. After deploying a migration that adds
a materialized helper table, include a backfill statement that touches every existing row so the
trigger populates the helper table for them. A no-op update works well:

```sql
update users set data = data;
```

This ensures constraints and lookups against the helper table are consistent immediately after the
migration, without requiring a separate backfill script.

Helper tables do not follow the standard DAO schema, so the database schema test will flag them
unless they are explicitly excluded. When adding a new helper table, update the `CheckSchema` method
in `internal/database/database_tool.go` so it is skipped during schema validation.

When a trigger raises an exception with a custom SQLSTATE code (the `Z` class is reserved for
user-defined conditions), the Go layer must also be updated so the error is translated before it
reaches gRPC clients. For each new code:

1. Add a constant in `internal/database/dao/dao_errors.go` following the pattern of the existing
   `errImmutableCode` (`Z0001`).
2. Add a corresponding `case` in the `translateError` functions in `generic_dao_create.go` and/or
   `generic_dao_update.go` (whichever operations the trigger can fire on) to map the PostgreSQL
   error to the appropriate domain error type (e.g., `ErrDenied`, `ErrReference`).

Without this step, the raw PostgreSQL error will propagate as an internal error to callers.

### gRPC Interceptor Chain

The gRPC server uses chained interceptors (configured in `internal/cmd/service/start/grpcserver/`):
1. Panic recovery
2. Prometheus metrics
3. Structured logging (slog)
4. Authentication (JWT validation)
5. Database transaction management

### Mock Generation

Uses `go.uber.org/mock` (uber-go/mock). Mocks are generated with `//go:generate mockgen` directives and live alongside source files (e.g., `attribution_logic_mock.go`).

### Testing Pattern

Tests use Ginkgo v2 + Gomega. Typical suite setup in `*_suite_test.go`:
- `BeforeSuite` initializes logger, auth logic, database
- `DeferCleanup` for teardown
- `dao.CreateTables[T]()` dynamically creates test schemas

## Automated Hooks

The following automated checks are configured and should be run at the appropriate times:

- **After proto changes**: When a `.proto` file is edited, run `buf lint && buf generate` to keep generated Go code in sync.
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
