# Catalog Items

## Overview

OSAC uses a three-level hierarchy to provision infrastructure resources:

```
Template (private API)
    ↓  referenced by
Catalog Item (private API, published to public API)
    ↓  used by
Resource (public API — cluster or compute instance)
```

**Templates** are infrastructure blueprints managed through the private (admin) API. They define
the underlying provisioning logic: node set structures, host types, and spec defaults (pull secret,
SSH key, release image, network CIDRs). Templates are not visible to end users.

**Catalog items** reference a template and add a curation layer on top. Using `field_definitions`,
the admin controls which resource spec fields are exposed to end users, locks fields to fixed
values, sets defaults, and optionally validates user input with JSON Schema. Once published
(`published: true`), catalog items become visible through the public API.

**Resources** (clusters, compute instances) are what end users create. A user can create a resource
in two ways:

- `osac create cluster --template <id>` — direct template access, no field restrictions. Supports
  `--template-parameter` to pass custom values (e.g., `vpc_id`, `vlan`) that AAP uses for
  provisioning.
- `osac create cluster --catalog-item <id>` — the server resolves the template from the catalog
  item and applies `field_definitions` to enforce defaults and validation. Only the fixed set of
  known fields can be controlled; custom template parameters are not supported.

Both templates and catalog items are created and managed by the platform admin through the private
API. The distinction is not one of roles but of purpose: templates define what infrastructure
exists, catalog items define what users see. Unlike templates, which are private-only, catalog items
span both APIs — the admin manages them via the private API, and once published they become visible
to end users via the public API (list, inspect, and use when creating resources).

### What field_definitions can and cannot do

Catalog items control a **fixed set of known fields** on the resource (e.g., `pull_secret`,
`ssh_public_key`, `network.pod_cidr` for clusters). These are the same fields available as CLI
flags (`--pull-secret`, `--ssh-public-key`, `--pod-cidr`).

**You cannot define custom parameters in a catalog item.** For example, you cannot create a
`field_definition` with `path: vlan` or `path: vpc_id` and expect AAP to receive it. Paths must
correspond to known fields in the resource spec — unknown paths have no effect on provisioning.

Custom provisioning parameters (like `vpc_id`, `ip_block_id`, `ssh_key_group_id`) are defined as
**template parameters**, which are a separate mechanism. Template parameters are passed by the user
with `--template-parameter` and forwarded to AAP as extra variables. However, `--template-parameter`
is **not supported with `--catalog-item`**, which means:

- Catalog items work well with templates that only need the standard spec fields.
- Templates that require custom parameters (e.g., `osac.templates.ocp_4_20_small_nico`) cannot be
  fully used through catalog items — users must use `--template` directly instead.

## Creating Catalog Items

Catalog items are created using `osac create -f` with a YAML file. The file must include an `@type`
field that identifies the catalog item type.

> **Note:** Unlike other resources (clusters, compute instances), catalog items do not have a
> dedicated `osac create <type>` subcommand. The `field_definitions` structure — a list of entries
> each with `path`, `editable`, `default`, and `validation_schema` — is too complex to express as
> CLI flags. Use `osac create -f` with a YAML file instead.

### ClusterTemplates

Cluster Catalog Items are based on Cluster Templates, you can list the ones available in your environment:

```bash
$ osac get clustertemplates
ID                                    NAME  TITLE
ocp_4_17_small                        -     OpenShift 4.17 small
osac.templates.ocp_4_17_small         -     Simple OpenShift 4.17 Cluster
osac.templates.ocp_4_17_small_github  -     OpenShift 4.17 Cluster + GitHub
osac.templates.ocp_4_20_small_nico    -     OpenShift 4.20 Cluster on NICo Bare Metal
osac.templates.ocp_ci_small           -     CI OpenShift Cluster
```

### ClusterCatalogItem

```yaml
'@type': type.googleapis.com/osac.public.v1.ClusterCatalogItem
metadata:
  name: dev-sandbox
title: Dev Sandbox Cluster
description: Small development cluster with locked-down defaults.
template: "osac.templates.ocp_4_17_small"
published: true
field_definitions:
  - path: ssh_public_key
    display_name: SSH Public Key
    editable: false
    default: "ssh-ed25519 AAAA..."
  - path: node_sets.workers.size
    display_name: Workers Count
    editable: false
    default: 1
  - path: node_sets.workers.host_type
    display_name: Workers Resource Class
    editable: true
    default: "fc430"
  - path: release_image
    display_name: Release Image
    editable: false
    default: "quay.io/openshift-release-dev/ocp-release:4.17.0-multi"
  - path: network.pod_cidr
    display_name: Pod CIDR
    editable: true
    default: "10.128.0.0/14"
    validation_schema: '{"type":"string","pattern":"^[0-9./]+$"}'
```

### ComputeInstanceTemplates

Compute Instance Catalog Items are based on Compute Instance Templates, you can list the ones available in your environment:

```bash
$ osac get computeinstancetemplates
ID                          NAME  TITLE
osac.templates.ocp_virt_vm  -     Virtual Machine Template (Linux and Windows)
```

### ComputeInstanceCatalogItem

```yaml
'@type': type.googleapis.com/osac.public.v1.ComputeInstanceCatalogItem
metadata:
  name: standard-vm
title: Standard Virtual Machine
description: General-purpose virtual machine with KubeVirt.
template: "osac.templates.ocp_virt_vm"
published: true
field_definitions:
  - path: ssh_key
    display_name: SSH Key
    editable: true
    default: "ssh-ed25519 AAAA..."
  - path: image.source_type
    default: registry
    display_name: Image Source Type
  - path: image.source_ref
    default: quay.io/containerdisks/fedora:latest
    display_name: Image Reference
  - path: boot_disk.size_gib
    default: 20
    display_name: Boot Disk Size (GiB)
    editable: true
  - path: instance_type
    display_name: Instance Type
    editable: true
  - path: run_strategy
    display_name: Run Strategy
    editable: true
  - path: network_attachments
    display_name: Network Attachments
    editable: true
```
> IMPORTANT: Notice that ComputeInstanceCatalogItem uses instance type. So, in order to use it you'll need to have an instance type. For example:

```yaml
'@type': type.googleapis.com/osac.private.v1.InstanceType
id: simple-1-2
metadata:
  creator: system
  name: simple-1-2
  tenant: shared
spec:
  cores: 1
  memory_gib: 2
  state: INSTANCE_TYPE_STATE_ACTIVE
```

### Create the catalog item

```bash
osac create -f newclustercatalogitem.yaml
```

The input file can contain multiple documents separated by `---`.

## Field Definitions

Each entry in `field_definitions` controls a single field on the resource spec:

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Dot-notation path referencing a field in the resource spec (e.g., `node_sets.workers.size`) |
| `display_name` | string | Human-friendly label for UIs and CLIs |
| `editable` | bool | Whether users can override this field when creating a resource from the catalog item |
| `default` | any | Default value for this field |
| `validation_schema` | string | Optional JSON Schema (draft 2020-12) for validating user-provided values of editable fields |

Non-editable fields are locked to their default value. Editable fields allow users to override the
default, optionally constrained by a `validation_schema`.

### Available Paths

The following paths can be used in `field_definitions`. When a user creates a resource from a
catalog item, the server rejects any spec field not listed in `field_definitions` with an
`InvalidArgument` error.

**ClusterCatalogItem** paths:

| Path | CLI flag | Description |
|------|----------|-------------|
| `pull_secret` | `--pull-secret` | Credentials for container image repositories |
| `ssh_public_key` | `--ssh-public-key` | SSH public key installed on worker nodes |
| `release_image` | `--release-image` | OCP release image URL |
| `network.pod_cidr` | `--pod-cidr` | Pod network CIDR (default: `10.128.0.0/14`) |
| `network.service_cidr` | `--service-cidr` | Service network CIDR (default: `172.30.0.0/16`) |
| `node_sets.<name>.size` | — | Number of nodes in a named node set (e.g., `node_sets.workers.size`) |
| `node_sets.<name>.host_type` | — | Host type for a named node set. Immutable after creation |

**ComputeInstanceCatalogItem** paths:

| Path | Description |
|------|-------------|
| `ssh_key` | SSH public key |
| `instance_type` | Instance Type includes number of CPU cores, memory, etc. |
| `run_strategy` | VM run strategy (e.g., `Always`, `Halted`) |
| `user_data` | Cloud-init or ignition user data |
| `image.source_type` | Image source type (e.g., `registry`) |
| `image.source_ref` | Image reference (e.g., OCI image URL) |
| `boot_disk.size_gib` | Boot disk size in GiB |
| `additional_disks` | Additional disk configurations |
| `network_attachments` | Network attachments (subnet + security groups per NIC) |

These paths are defined by the resource API. If new fields are added or removed in a future version,
the available paths change accordingly.

## Creating Resources from Catalog Items

Once a catalog item is published, users create resources from it using `--catalog-item`:

```bash
osac create cluster --catalog-item dev-sandbox
```

Users can provide spec fields via CLI flags:

```bash
osac create cluster --catalog-item dev-sandbox \
  --name my-cluster \
  --pull-secret "$(cat pull-secret.json)" \
  --ssh-public-key "$(cat ~/.ssh/id_ed25519.pub)" \
  --release-image "quay.io/openshift-release-dev/ocp-release:4.17.0-multi" \
  --pod-cidr "10.128.0.0/14"
```

> **Note:** `--template-parameter` is not supported with `--catalog-item`. For templates that
> require custom parameters, use `--template` directly (see
> [What field_definitions can and cannot do](#what-field_definitions-can-and-cannot-do)).

### How CLI flags interact with field_definitions

CLI flags like `--pull-secret` and `--ssh-public-key` set values in the resource spec. However,
the server applies `field_definitions` **after** receiving the request, which means:

- If a field is **not listed** in `field_definitions`, the server **rejects** the request with
  `InvalidArgument`.
- If a field is **non-editable** in the catalog item and the user provides a value, the server
  **rejects** the request with `InvalidArgument`.
- If a field is **editable**, the CLI flag value is accepted and validated against
  `validation_schema` if one is defined.
- If an editable field is **not provided** by the user, the catalog item's default is applied.

For example, given a catalog item with `release_image` set as non-editable with a fixed default,
running `--release-image "quay.io/user1/ocp-release:4.12.0"` results in an error — the user cannot override locked fields.

### Server-side processing

When the server processes the request, it:

1. Looks up the catalog item and verifies it is published
2. Sets the resource's `spec.template` to the template referenced by the catalog item
3. Applies `field_definitions`:
   - **Unlisted fields**: any spec field not in `field_definitions` is rejected (`InvalidArgument`)
   - **Non-editable fields**: rejects if the user provided a value; otherwise applies the default
   - **Editable fields with a user value**: validated against `validation_schema` if present
   - **Editable fields without a user value**: the default is applied
4. Validates the resulting spec and creates the resource

## Managing Catalog Items

### List

```bash
osac get clustercatalogitems
osac get computeinstancecatalogitems
```

### Inspect

```bash
osac get clustercatalogitems <id> -o yaml
osac get computeinstancecatalogitems <id> -o yaml
```

### Update

Edit a catalog item interactively (opens in `$EDITOR`):

```bash
osac edit clustercatalogitems <id>
osac edit computeinstancecatalogitems <id>
```

This lets you modify `title`, `description`, `published`, `field_definitions`, and `template`.

### Delete

```bash
osac delete clustercatalogitems <id>
osac delete computeinstancecatalogitems <id>
```

## API Endpoints

| Operation | Method | Endpoint |
|-----------|--------|----------|
| List cluster catalog items | `GET` | `/api/fulfillment/v1/cluster_catalog_items` |
| Get cluster catalog item | `GET` | `/api/fulfillment/v1/cluster_catalog_items/{id}` |
| Create cluster catalog item | `POST` | `/api/fulfillment/v1/cluster_catalog_items` |
| Update cluster catalog item | `PATCH` | `/api/fulfillment/v1/cluster_catalog_items/{id}` |
| Delete cluster catalog item | `DELETE` | `/api/fulfillment/v1/cluster_catalog_items/{id}` |
| List compute instance catalog items | `GET` | `/api/fulfillment/v1/compute_instance_catalog_items` |
| Get compute instance catalog item | `GET` | `/api/fulfillment/v1/compute_instance_catalog_items/{id}` |
| Create compute instance catalog item | `POST` | `/api/fulfillment/v1/compute_instance_catalog_items` |
| Update compute instance catalog item | `PATCH` | `/api/fulfillment/v1/compute_instance_catalog_items/{id}` |
| Delete compute instance catalog item | `DELETE` | `/api/fulfillment/v1/compute_instance_catalog_items/{id}` |

See [Filter expressions](FILTER.md) for filtering and ordering list results.
