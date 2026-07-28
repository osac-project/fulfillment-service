# Catalog Item Examples

This directory contains example catalog items that can be created in the OSAC fulfillment service. These YAML files can be used directly with `osac create -f` for one-off creation, or as reference for the `osac-dev seed-catalog-items` idempotent seeding command.

## Contents

- `simple-ocp-4-17-cluster.yaml` — Simple OpenShift 4.17 cluster (fc430 hardware)
- `ocp-4-20-nico-baremetal-cluster.yaml` — OpenShift 4.20 cluster on NICo bare metal
- `linux-vm.yaml` — General-purpose Linux VM
- `windows-vm.yaml` — Windows VM

## Usage

### Using the osac CLI

Catalog items are part of the **private API**, so you must first log in with the `--private` flag to enable private API access:

```bash
# Log in with private API access enabled
osac login --private ...

# Create each catalog item
osac create -f simple-ocp-4-17-cluster.yaml
osac create -f ocp-4-20-nico-baremetal-cluster.yaml
osac create -f linux-vm.yaml
osac create -f windows-vm.yaml
```

**Note:** The `osac create -f` command is **not idempotent** — it will fail if the catalog item already exists.

### Using the osac-dev CLI (idempotent)

For idempotent seeding (safe to run multiple times), use the `osac-dev seed-catalog-items` subcommand:

```bash
go run ./cmd/osac-dev seed-catalog-items \
  --api-url=fulfillment-api.osac.svc.cluster.local:443 \
  --token=$(kubectl create token -n osac client) \
  --insecure
```

This command:
- Creates all 4 catalog items if they don't exist
- Skips items that already exist (by name)
- Connects directly to the gRPC API

## Authentication

Both methods require authentication:

- **osac CLI**: Set up authentication with `osac login` first, or use `--token` flag
- **osac-dev CLI**: Requires `--api-url` and `--token` flags

Example token generation for development:

```bash
kubectl create token -n osac client
```

## File Format

These YAML files use the protobuf `Any` encoding format required by `osac create -f`. Each file includes:

- `@type` field: Identifies the protobuf message type (e.g., `type.googleapis.com/osac.private.v1.ClusterCatalogItem`)
- `metadata.name`: Unique identifier for the catalog item
- `title`: Human-friendly display name
- `description`: Detailed description (supports Markdown)
- `template`: Template identifier this catalog item references
- `published`: Whether visible in the public API
- `field_definitions`: List of user-editable fields with validation schemas
