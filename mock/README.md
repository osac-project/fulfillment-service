# Fulfillment Service Mock

A lightweight mock server for the OSAC fulfillment-service REST API. It reads the
OpenAPI v3 spec at startup, auto-discovers all resource types, and serves them with
in-memory state, JWT authentication, state machine progression, and SSE events.

## Quick start

```bash
cd fulfillment-service/mock

# Create a virtual environment and install dependencies
python3 -m venv .venv
.venv/bin/pip install -e .

# Run with default scenario (pre-populated resources)
.venv/bin/python mock_server.py --scenario scenarios/default.yaml

# Run without auth (all requests treated as admin)
.venv/bin/python mock_server.py --no-auth --scenario scenarios/default.yaml
```

The server starts on `http://localhost:8000`.

## Authentication

The mock includes a complete OIDC-compatible auth flow.

### Get a token

```bash
curl -X POST http://localhost:8000/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username": "adam", "password": "adam"}'
```

### Use the token

```bash
export TOKEN="<access_token from above>"
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/fulfillment/v1/virtual_networks
```

### Pre-configured users

| Username  | Password  | Tenants              | Role                 |
|-----------|-----------|----------------------|----------------------|
| `admin`   | `admin`   | all                  | cloud-provider-admin |
| `adam`     | `adam`    | engineering          | tenant-admin         |
| `ben`     | `ben`     | engineering, sales   | tenant-user          |
| `charles` | `charles` | sales                | tenant-user          |

### OIDC discovery

- `GET /.well-known/openid-configuration` — OIDC discovery document
- `GET /.well-known/jwks.json` — JSON Web Key Set
- `GET /api/fulfillment/v1/capabilities` — Auth configuration (no auth required)

## API usage

All resource types from the fulfillment-service are available with standard CRUD:

```bash
# List
curl $AUTH http://localhost:8000/api/fulfillment/v1/virtual_networks

# Get
curl $AUTH http://localhost:8000/api/fulfillment/v1/virtual_networks/{id}

# Create
curl -X POST $AUTH -H "Content-Type: application/json" \
  http://localhost:8000/api/fulfillment/v1/virtual_networks \
  -d '{"metadata":{"name":"my-net"},"spec":{"network_class":"nc-udn-001","ipv4_cidr":"10.0.0.0/16"}}'

# Update
curl -X PATCH $AUTH -H "Content-Type: application/json" \
  http://localhost:8000/api/fulfillment/v1/virtual_networks/{id} \
  -d '{"metadata":{"name":"renamed-net"}}'

# Delete
curl -X DELETE $AUTH http://localhost:8000/api/fulfillment/v1/virtual_networks/{id}
```

Where `$AUTH` is `-H "Authorization: Bearer $TOKEN"`.

## State progression

Stateful resources automatically progress through their lifecycle after creation:

| Resource           | Transition                     | Delay |
|--------------------|--------------------------------|-------|
| VirtualNetwork     | PENDING → READY                | 1s    |
| Subnet             | PENDING → READY                | 1s    |
| SecurityGroup      | PENDING → READY                | 0.5s  |
| Cluster            | PROGRESSING → READY            | 2s    |
| ComputeInstance     | STARTING → RUNNING             | 1.5s  |
| BareMetalInstance   | PROVISIONING → RUNNING         | 3s    |
| PublicIP           | PENDING → ALLOCATED            | 1s    |
| PublicIPAttachment  | PENDING → READY                | 0.5s  |

## Failure injection

Use labels with the `mock.osac.dev/` prefix to control mock behavior:

| Label                              | Effect                                      |
|------------------------------------|---------------------------------------------|
| `mock.osac.dev/fail-provision`     | Resource transitions to FAILED              |
| `mock.osac.dev/provision-delay`    | Custom delay (e.g., `"5s"`, `"500ms"`)      |
| `mock.osac.dev/fail-after`         | Reach READY, then fail after delay           |
| `mock.osac.dev/fail-create`        | Create request returns 400                   |
| `mock.osac.dev/fail-delete`        | Delete request returns 500                   |
| `mock.osac.dev/error-message`      | Custom message in status.message             |

Example — create a VM that fails to provision:

```bash
curl -X POST $AUTH -H "Content-Type: application/json" \
  http://localhost:8000/api/fulfillment/v1/compute_instances \
  -d '{
    "metadata": {
      "name": "doomed-vm",
      "labels": {
        "mock.osac.dev/fail-provision": "true",
        "mock.osac.dev/error-message": "Insufficient host capacity"
      }
    },
    "spec": {"instance_type": "it-large-001"}
  }'
```

## SSE events

Connect to the event stream to receive real-time notifications:

```bash
curl -N $AUTH http://localhost:8000/api/events/v1/events
```

Events are emitted when resources are created, updated (including state transitions),
or deleted. Each event includes the full resource object.

## Scenarios

Scenario files (YAML) pre-populate the mock with resources in various states.
The default scenario includes networking, compute, clusters, and identity resources
across the `engineering` and `sales` tenants.

```bash
# Load a specific scenario
.venv/bin/python mock_server.py --scenario scenarios/default.yaml

# Load multiple scenarios
.venv/bin/python mock_server.py --scenario scenarios/default.yaml --scenario scenarios/extra.yaml

# Start empty
.venv/bin/python mock_server.py
```

See `scenarios/default.yaml` for the format.

## CLI options

```text
--port PORT        Port to listen on (default: 8000)
--spec PATH        Path to OpenAPI v3 spec (default: ../pages/openapi/v3/public.yaml)
--scenario PATH    Scenario YAML file to preload (repeatable)
--no-auth          Disable JWT authentication
```

## Container image

Build from the `fulfillment-service/` root:

```bash
podman build -f mock/Containerfile -t fulfillment-mock .
podman run -p 8000:8000 fulfillment-mock
```

## Updating for API changes

The mock auto-discovers routes from the OpenAPI spec. When proto definitions change:

1. Run `buf generate` in `fulfillment-service/` to regenerate the OpenAPI spec
2. Restart the mock server — new resource types appear automatically

Manual updates are only needed when adding a new **stateful** resource type
(one that needs state machine transitions). Add an entry to `STATE_MACHINES`
in `state_machines.py`.
