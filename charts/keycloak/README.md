# Keycloak Helm chart

This Keycloak Helm chart is intended for use in the integration tests of the fulfillment service
inside a _kind_ cluster. It provides a pre-configured Keycloak instance with the necessary realm and
client configurations for testing authentication and authorization workflows.

## Installation

Before installing this chart you will need a working installation of _cert-manager_ and at least one
issuer defined.

The following table lists the configurable parameters of the Keycloak chart:

| Parameter              | Description                                                   | Required | Default         |
|------------------------|---------------------------------------------------------------|----------|-----------------|
| `variant`              | Deployment variant (`openshift` or `kind`)                    | No       | `kind`          |
| `hostname`             | The hostname that Keycloak uses to refer to itself            | **Yes**  | None            |
| `certs.issuerRef.kind` | The kind of cert-manager issuer (`ClusterIssuer` or `Issuer`) | No       | `ClusterIssuer` |
| `certs.issuerRef.name` | The name of the cert-manager issuer for TLS certificates      | **Yes**  | None            |
| `images.keycloak`      | The Keycloak container image                                  | No       | `26.3`          |
| `images.postgres`      | The PostgreSQL container image                                | No       | `15`            |
| `groups`               | List of groups to create in the Keycloak realm                | No       | `[]`            |
| `users`                | List of users to create in the Keycloak realm                 | No       | `[]`            |

Note specially that the `hostname` and `certs.issuerRef.name` parameters are required. For example,
in the integration tests environment the chart is installed like this:

```bash
$ helm install keycloak charts/keycloak \
--namespace keycloak \
--create-namespace \
--set hostname=keycloak.keycloak.svc.cluster.local \
--set certs.issuerRef.name=default-ca \
--wait
```

To uninstall it:

```bash
$ helm uninstall keycloak --namespace keycloak
```

Here's an example `values.yaml` file for installing the chart:

```yaml
variant: kind

hostname: keycloak.osac

certs:
  issuerRef:
    kind: ClusterIssuer
    name: default-ca
```

Install using a values file:

```bash
$ helm install keycloak charts/keycloak \
--namespace keycloak \
--create-namespace \
--values values.yaml \
--wait
```

## Configuring groups and users

The chart allows you to create groups and users in the Keycloak realm by specifying them in the
`values.yaml` file. These are merged into the base realm configuration during deployment.

### Groups

Groups are specified as a list with at least the `name` and `path` fields:

```yaml
groups:

- name: admins
  path: /admins

- name: developers
  path: /developers
```

### Users

Users require more fields to be functional. The most important fields are:

- `username`: The login name for the user.
- `enabled`: Must be `true` for the user to log in.
- `credentials`: List of credentials with `type`, `value`, and `temporary` fields.
- `temporary`: Must be `false` in credentials, otherwise password grant won't work.

Example with users and groups:

```yaml
groups:

- name: admins
  path: /admins

users:

- username: alice
  enabled: true
  emailVerified: true
  credentials:
  - type: password
    value: alice123
    temporary: false
  firstName: Alice
  lastName: Smith
  email: alice@example.com
  groups:
  - /admins

- username: bob
  enabled: true
  emailVerified: true
  credentials:
  - type: password
    value: bob456
    temporary: false
  firstName: Bob
  lastName: Jones
  email: bob@example.com
```

Refer to the [Keycloak documentation](https://www.keycloak.org/docs/latest/server_admin/) for more
details about the available fields for groups and users.

## Exporting the realm

To export the realm configuration to a JSON file, you need to find the Keycloak pod and execute the
`export` command inside it. The exported data can be written to a local JSON file using the
following steps:

1. First, find the name of the Keycloak pod:

    ```bash
    $ pod=$(kubectl get pods -n keycloak -l app=keycloak-service -o json | jq -r '.items[].metadata.name')
    ```

2. Run the `export` command inside the pod to write the ream to a temporary file:

    ```bash
    $ kubectl exec -n keycloak "${pod}" -- /opt/keycloak/bin/kc.sh export --realm osac --file /tmp/realm.json
    ```

3. Copy the temporary file to a local file:

    ```bash
    $ kubectl exec -n keycloak "${pod}" -- cat /tmp/realm.json > realm.json
    ```

4. Optionally, if you want to replace the realm used by the chart, overwrite the
   `realm.json` file:

   ```bash
   $ cp realm.json charts/keycloak/files/realm.json
   ```
