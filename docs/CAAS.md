# Cluster-as-a-Service (CaaS) User Guide

This guide describes how tenants can create, manage, and delete OpenShift clusters through the
OSAC CLI and API.

## Prerequisites

- `osac` installed and authenticated (`osac login`)
- A cluster template published by your provider

## Workflow Overview

1. Browse available cluster templates
2. Create a cluster from a template
3. Wait for the cluster to become ready
4. Access the cluster (kubeconfig, console, admin password)
5. Scale nodes as needed
6. Delete the cluster when done

## Browse Cluster Templates

List all available templates:

```bash
osac get cluster-templates
```

Inspect a specific template to see its parameters and default node sets:

```bash
osac get cluster-templates <template-id> -o yaml
```

Key fields in a template:

- **`parameters`**: Input parameters for cluster creation. Each parameter has a `name`, `type`,
  whether it is `required`, and optionally a `default` value.
- **`node_sets`**: Default node set configuration. Each node set specifies a `host_class` (hardware
  type) and initial `size` (number of nodes).

## Create a Cluster

Create a cluster using a template, providing any required parameters:

```bash
osac create cluster \
  --template hosted_cluster \
  --template-parameter cluster_version=4.16
```

Optional flags:

- `-n, --name <name>` - Human-readable name for the cluster
- `-p, --template-parameter <name=value>` - Template parameter (repeatable)
- `-f, --template-parameter-file <name=filename>` - Template parameter from file (repeatable)

The command outputs the cluster ID upon successful creation.

If you don't specify `node_sets`, the template defaults are used. Node sets can be customized
after creation via `edit`.

## Check Cluster Status

List all your clusters:

```bash
osac get clusters
```

The table shows ID, name, template, state, API URL, and console URL.

Get detailed information about a specific cluster:

```bash
osac describe cluster <cluster-id>
```

Or in full YAML format:

```bash
osac get clusters <cluster-id> -o yaml
```

### Cluster States

| State | Meaning |
|-------|---------|
| `CLUSTER_STATE_PROGRESSING` | Cluster is being created or updated |
| `CLUSTER_STATE_READY` | Cluster is operational and accessible |
| `CLUSTER_STATE_FAILED` | Cluster creation or update failed |

Deletion is indicated by a `deletion_timestamp` in the cluster metadata rather than a separate
state.

### Conditions

A cluster in `READY` state may have additional conditions:

- **`DEGRADED`** = `TRUE`: Cluster is operational but not at full capacity (e.g., some requested
  worker nodes could not be allocated). The control plane is functional.

## Access the Cluster

Once the cluster is in `READY` state, retrieve credentials:

**Kubeconfig** (for `oc` / `kubectl`):

```bash
osac get kubeconfig <cluster-id> > kubeconfig.yaml
export KUBECONFIG=kubeconfig.yaml
oc get nodes
```

**Admin password** (for the web console):

```bash
osac get password <cluster-id>
```

The console URL is shown in `get clusters` output or in the cluster's `status.console_url` field.

## Scale Nodes

To change node set sizes, use the generic edit command:

```bash
osac edit clusters <cluster-id>
```

This opens the cluster resource in your `$EDITOR`. Modify the `size` field under
`spec.node_sets.<node-set-name>` and save.

**Constraints:**

- The `host_class` of an existing node set cannot be changed (immutable after creation)
- New node sets can be added
- At least one node set must remain
- Node sets can be scaled to zero (the control plane continues to run on the hub)

After saving, the cluster transitions to `PROGRESSING` until the new node configuration is applied.

## Delete a Cluster

```bash
osac delete clusters <cluster-id>
```

Deletion triggers cleanup of all associated resources (HostedCluster, HostPool, bare-metal host
allocations). The cluster remains visible with a `deletion_timestamp` set until cleanup completes.

Verify deletion:

```bash
osac get clusters
```

## API Access

All CLI operations correspond to REST API endpoints:

| Operation | Method | Endpoint |
|-----------|--------|----------|
| List clusters | `GET` | `/api/fulfillment/v1/clusters` |
| Get cluster | `GET` | `/api/fulfillment/v1/clusters/{id}` |
| Create cluster | `POST` | `/api/fulfillment/v1/clusters` |
| Update cluster | `PATCH` | `/api/fulfillment/v1/clusters/{id}` |
| Delete cluster | `DELETE` | `/api/fulfillment/v1/clusters/{id}` |
| Get kubeconfig | `GET` | `/api/fulfillment/v1/clusters/{id}/kubeconfig` |
| Get password | `GET` | `/api/fulfillment/v1/clusters/{id}/password` |
| List templates | `GET` | `/api/fulfillment/v1/cluster_templates` |
| Get template | `GET` | `/api/fulfillment/v1/cluster_templates/{id}` |

See [Filter expressions](FILTER.md) for filtering and ordering list results.
