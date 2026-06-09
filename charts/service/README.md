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
$ helm install fulfillment-service ./charts/service -n osac --create-namespace
```

## Configuration

The following table lists the configurable parameters of the chart and their default values:

| Parameter                  | Description                                                        | Default                                                        |
|----------------------------|--------------------------------------------------------------------|----------------------------------------------------------------|
| `variant`                  | Deployment variant (`kind` or `openshift`)                         | `kind`                                                         |
| `certs.issuerRef.kind`     | Kind of _cert-manager_ issuer                                      | `ClusterIssuer`                                                |
| `certs.issuerRef.name`     | Name of _cert-manager_ issuer                                      | None                                                           |
| `certs.caBundle.configMap` | Name of configmap containing trusted CA certificates in PEM format | Required                                                       |
| `externalHostname`         | Hostname used to access the public API from outside the cluster (see note below) | None                                                |
| `internalHostname`         | Hostname used to access both the public and private APIs (see note below)        | None                                                |
| `auth.issuerUrl`           | In-cluster OAuth issuer URL for token validation                   | None |
| `auth.externalIssuerUrl`   | External OAuth issuer URL for browsers and external CLI clients    | None                                                           |
| `log.level`                | Log level for all components (debug, info, warn, error)            | `info`                                                         |
| `log.headers`              | Enable logging of HTTP/gRPC headers                                | `false`                                                        |
| `log.bodies`               | Enable logging of HTTP/gRPC request and response bodies            | `false`                                                        |
| `images.service`           | Fulfillment service container image                                | `ghcr.io/osac/fulfillment-service:main`                        |
| `images.ui`                | UI container image                                                 | `ghcr.io/osac/osac-ui:main`                                    |
| `images.envoy`             | Envoy proxy container image                                        | `docker.io/envoyproxy/envoy:v1.37.1`                           |
| `database.connection`      | List of sources for database connection parameters (see below)     | `[]`                                                           |
| `ui.enabled`               | Deploy the UI as part of this chart                                | `true`                                                         |
| `ui.hostname`              | External hostname for the UI                                       | None                                                           |
| `ui.oidcClientId`          | OIDC client ID registered in Keycloak for the UI                   | `osac-ui`                                                      |
**Note on `internalHostname`:** The internal API exposes both the public and private APIs. The
administrator is responsible for ensuring that this hostname is accessible only from the internal
network and not accessible to regular users. This isn't strictly required because access is subject
to authentication and authorization (regular users won't be authorized to use the private API), but
it is good practice to restrict network-level access as an additional layer of defense. When
`internalHostname` is not set, no Route (OpenShift) or TLSRoute (Kind) will be created for the
internal API, allowing the administrator to manually configure ingress with custom settings such as
a dedicated load balancer or a specific IP address.

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

externalHostname: fulfillment-service.example.com
internalHostname: fulfillment-internal.example.com

auth:
  issuerUrl: https://keycloak.example.com/realms/osac

log:
  level: debug
  headers: true
  bodies: true

images:
  service: ghcr.io/osac/fulfillment-service:v1.0.0

database:
  connection:
  - configMap:
      name: fulfillment-database-config
      items:
      - key: url
        param: url
  - secret:
      name: fulfillment-database-client-cert
      items:
      - key: tls.crt
        param: sslcert
      - key: tls.key
        param: sslkey
      - key: ca.crt
        param: sslrootcert
```

Then install with:

```bash
$ helm install fulfillment-service ./charts/service -n osac -f values.yaml
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
helm uninstall fulfillment-service -n osac
```

## Database

The chart expects an external PostgreSQL database to be available. The database
connection details are provided via `database.connection`, a list of ConfigMap
and Secret sources that provide the connection parameters. Each entry maps keys
from a ConfigMap or Secret to connection parameters.
