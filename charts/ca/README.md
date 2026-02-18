# CA Helm chart

This Helm chart creates a self-signed CA certificate and a `ClusterIssuer` named `default-ca` that can be used by other
components in the _OSAC_ project.

The CA certificate is copied to a config map named `ca-bundle` into all the namespaces of the cluster, in the
`bundle.pem` key.

## Installation

Before installing this chart you will need to install the _cert-manager_ and _trust-manager_ operators.

To install the chart with the default configuration:

```shell
$ helm install default-ca charts/ca \
--namespace cert-manager
```

To install with custom values:

```shell
$ helm install default-ca charts/ca \
--namespace cert-manager \
--set issuerName=my-issuer
```

## Configuration

The following table lists the configurable parameters of the CA chart and their default values:

| Parameter    | Description                               | Default       |
|--------------|-------------------------------------------|---------------|
| `issuerName` | The name of the `ClusterIssuer` to create | `default-ca`  |
| `bundleName` | The name of the bundle to create          | `ca-bundle`   |
| `commonName` | The common name for the CA certificate    | `OSAC CA`  |

The namespace can also be changed using the `--namespace` flag, but it must match the namespace
where _cert-manager_ stores the secrets for cluster issuers, which is usually `cert-manager`.

## Usage

The `ClusterIssuer` can be referenced by certificate resources throughout the cluster using something like this:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  namespace: my-namespace
  name: my-certificate
spec:
  issuerRef:
    kind: ClusterIssuer
    name: default-ca
  dnsNames:
  - my-server.example.com
  secretName: my-server-tls
```

## Uninstallation

To uninstall the chart:

```shell
$ helm uninstall default-ca --namespace cert-manager
```

**Note:** This will remove the CA certificate and the issuer. Any certificates issued will continue to exist but cannot
be renewed.
