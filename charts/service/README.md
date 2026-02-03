# Service Helm Chart

This Helm chart deploys the complete fulfillment service.

## Prerequisites

- Kubernetes cluster (_Kind_ or _OpenShift_).
- _cert-manager_ operator installed.
- _trust-manager_ operator installed (optional, but highly recommended).
- _Authorino_ operator installed.

## Installation

To install the chart with the release name `fulfillment-service`:

```bash
$ helm install fulfillment-service ./charts/service -n innabox --create-namespace
```

## Configuration

The following table lists the configurable parameters of the chart and their default values:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `variant` | Deployment variant (`kind` or `openshift`) | `kind` |
| `certs.issuerRef.kind` | Kind of _cert-manager_ issuer | `ClusterIssuer` |
| `certs.issuerRef.name` | Name of _cert-manager_ issuer | None |
| `certs.caBundle.configMap` | Name of configmap containing trusted CA certificates in PEM format | Required |
| `hostname` | Hostname used to access the service from outside the cluster | None |
| `auth.issuerUrl` | OAuth issuer URL for authentication | `https://keycloak.keycloak.svc.cluster.local:8000/realms/innabox` |
| `log.level` | Log level for all components (debug, info, warn, error) | `info` |
| `log.headers` | Enable logging of HTTP/gRPC headers | `false` |
| `log.bodies` | Enable logging of HTTP/gRPC request and response bodies | `false` |
| `images.service` | Fulfillment service container image | `ghcr.io/innabox/fulfillment-service:main` |
| `images.postgres` | PostgreSQL container image | `quay.io/sclorg/postgresql-15-c9s:latest` |
| `images.envoy` | Envoy proxy container image | `docker.io/envoyproxy/envoy:v1.33.0` |
| `database.storageSize` | Size of database persistent volume | `10Gi` |

### Example custom values

To customize the deployment, create a `values.yaml` file:

```yaml
variant: openshift

certs:
  issuerRef:
    kind: Issuer
    name: my-issuer
  caBundle:
    configMap: my-ca-bundle

hostname: fulfillment-service.example.com

auth:
  issuerUrl: https://keycloak.example.com/realms/innabox

log:
  level: debug
  headers: true
  bodies: true

images:
  service: ghcr.io/innabox/fulfillment-service:v1.0.0

database:
  storageSize: 50Gi
```

Then install with:

```bash
$ helm install fulfillment-service ./charts/service -n innabox -f values.yaml
```

## Variants

### Kind variant

When `variant: kind` is set:
- The API service uses NodePort type with port 30000
- Suitable for development and testing

### OpenShift variant

When `variant: openshift` is set:
- The API service uses ClusterIP type
- An OpenShift Route is created for external access
- Suitable for production deployments on OpenShift

## Uninstallation

To uninstall the chart:

```bash
helm uninstall fulfillment-service -n innabox
```

## Database

The chart includes a complete _PostgreSQL_ database deployment.
