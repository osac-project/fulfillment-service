# Fulfillment service

This project contains the code for the fulfillment service.

## Required development tools

To work with this project you will need the following tools:

- [Go](https://go.dev) - Used to build the Go code.
- [Buf](https://buf.build) - Used to generate Go code from gRPC specifications.
- [Ginkgo](https://onsi.github.io/ginkgo) - Used to run unit tests.
- [gomock](https://github.com/uber-go/mock) - Used to generate test mocks.
- [Kustomize](https://kustomize.io) - Used to generate Kubernetes manifests.
- [Kubectl](https://kubernetes.io/es/docs/reference/kubectl) - Used to deploy to an OpenShift cluster.
- [PostgreSQL](https://www.postgresql.org) - Used to store persistent state.
- [Podman](https://podman.io) - Used to build and run container images.
- [gRPCurl](https://github.com/fullstorydev/grpcurl) - Used to test the gRPC API from the CLI.
- [curl](https://curl.se) - Used to test the REST API from the CLI.
- [jq](https://jqlang.org) - Used by some of the commands in this document.
- [kind](https://kind.sigs.k8s.io) - Used to create Kubernetes clusters for integration tests.

## Building the binary

To build the `fulfillment-service` binary run `go build`.

## Running unit tests

To run unit the unit tests run `ginkgo run -r`.

## Running PostgreSQL

To quickly run a local postgresql database in a container, run the following command:

```
podman run -d --name postgresql_database \
  -e POSTGRESQL_USER=user -e POSTGRESQL_PASSWORD=pass -e POSTGRESQL_DATABASE=db \
  -p 127.0.0.1:5432:5432 quay.io/sclorg/postgresql-15-c9s:latest
```

Done!

Or if you prefer to install and run postgresql directly on your development
system, you'll need to create a database for the service. For example, assuming
that you already have administrator access to the database, you can create a
user `user` with password `pass` and a database `db` with the following
commands:

    postgres=# create user user with password 'pass';
    CREATE ROLE
    postgres=# create database db owner user;
    CREATE DATABASE
    postgres=#

## Running the fulfillment-service

To run the the gRPC server use a command like this:

    $ ./fulfillment-service  start grpc-server \
    --log-level=debug \
    --log-headers=true \
    --log-bodies=true \
    --grpc-listener-address=localhost:8000 \
    --db-url=postgres://user:pass@localhost:5432/db

To run the the REST gateway use a command like this:

    $ ./fulfillment-service start rest-gateway \
    --log-level=debug \
    --log-headers=true \
    --log-bodies=true \
    --http-listener-address=localhost:8001 \
    --grpc-server-address=localhost:8000 \
    --grpc-server-plaintext

You may need to adjust the commands to use your database details.

To verify that the gRPC server is working use `grpcurl`. For example, to list the available gRPC services:

    $ grpcurl -plaintext localhost:8000 list
    fulfillment.v1.ClusterOrders
    fulfillment.v1.ClusterTemplates
    fulfillment.v1.Clusters
    fulfillment.v1.Events
    grpc.reflection.v1.ServerReflection
    grpc.reflection.v1alpha.ServerReflection

To list the methods available in a service, for example in the `ClusterTemplates` service:

    $ grpcurl -plaintext localhost:8000 list fulfillment.v1.ClusterTemplates
    fulfillment.v1.ClusterTemplates.Get
    fulfillment.v1.ClusterTemplates.List

To invoke a method, for example the `List` method of the `ClusterTemplates` service:

    $ grpcurl -plaintext localhost:8000 fulfillment.v1.ClusterTemplates/List
    {
      "size": 2,
      "total": 2,
      "items": [
        {
          "id": "my-template",
          "title": "My template",
          "description": "My template is *nice*."
        },
        {
          "id": "your-template",
          "title": "Your template",
          "description": "Your template is _ugly_."
        }
      ]
    }

To verify that the REST gateway is working use `curl`. For example, to get the list of templates:

    $ curl --silent http://localhost:8001/api/fulfillment/v1/cluster_templates | jq
    {
      "size": 2,
      "total": 2,
      "items": [
        {
          "id": "my-template",
          "title": "My template",
          "description": "My template is *nice*."
        },
        {
          "id": "your-template",
          "title": "Your template",
          "description": "Your template is _ugly_."
        }
      ]
}

## Building the container image

Select your image name, for example `quay.io/myuser/fulfillment-service:latest`, then build and tag the image with a
command like this:

    $ podman build -t quay.io/myuser/fulfillment-service:latest .

If you want to deploy to an OpenShift cluster then you will also need to push the image, so that the cluster can pull
it:

    $ podman push quay.io/myuser/fulfillment-service:latest

## Deploying to an OpenShift cluster

In order to be able to use gRPC in an OpenShift cluster it is necessary to enable HTTP/2:

    $ kubectl annotate ingresses.config/cluster ingress.operator.openshift.io/default-enable-http2=true

To deploy using the default image run the following command:

    $ kubectl apply -k manifests

To undeploy:

    $ kubectl delete -k manifests

If you want to deploy using your own image, then you will need first to edit the manifests:

    $ cd manifests
    $ kustomize edit set image fulfillment-service=quay.io/myuser/fulfillment-service:latest

The server deployed to the OpenShift cluster requires authentication. Before using it you will need to obtain the
token of the `client` service account that is created for that. Use a command like this to obtain the token and save it
into the `token` environment variable:

    $ export token=$(kubectl create token -n osac client)

To verify that the deployment is working get the URL of the route, and use `grpcurl` and `curl` to verify that both the
gRPC server and the REST gateway are working:

    $ kubectl get route -n osac fulfillment-api -o json | jq -r '.spec.host'
    fulfillment-api-osac.apps.mycluster.com

    $ grpcurl -insecure
    -H "Authorization: Bearer ${token}" \
    fulfillment-api-osac.apps.mycluster.com:443 fulfillment.v1.ClusterTemplates/List
    {
      "size": 2,
      "total": 2,
      "items": [
        {
          "id": "my-template",
          "title": "My template",
          "description": "My template is *nice*."
        },
        {
          "id": "your-template",
          "title": "Your template",
          "description": "Your template is _ugly_."
        }
      ]
    }

    $  curl --silent --insecure \
    --header "Authorization: Bearer ${token}" \
    https://fulfillment-api-osac.apps.mycluster.com:443/api/fulfillment/v1/cluster_templates | jq
    {
      "size": 2,
      "total": 2,
      "items": [
        {
          "id": "my-template",
          "title": "My template",
          "description": "My template is *nice*."
        },
        {
          "id": "your-template",
          "title": "Your template",
          "description": "Your template is _ugly_."
        }
      ]
    }

## Running integration tests

The project includes integration tests that run against a real Kubernetes cluster created using
[kind](https://kind.sigs.k8s.io). These tests verify the end-to-end functionality of the fulfillment
service by deploying it to a temporary cluster and exercising the APIs.

The integration tests use TLS with SNI (_Server Name Indication_) routing through the Envoy Gateway.
This means that the services are accessed using their Kubernetes internal host names, but routed
through `127.0.0.1:8000` which is exposed by the Kind cluster.

For the tests to work correctly, the following host names must resolve to `127.0.0.1`:

- `keycloak.keycloak.svc.cluster.local` - The Keycloak identity provider used for authentication.
- `fulfillment-api.osac.svc.cluster.local` - The fulfillment service API.

Add the following entries to your `/etc/hosts` file:

```
127.0.0.1 keycloak.keycloak.svc.cluster.local
127.0.0.1 fulfillment-api.osac.svc.cluster.local
```

To run the integration tests:

```bash
$ ginkgo run it
```

The integration tests will automatically:

1. Create a kind cluster named "fulfillment-service-it".
2. Build and load the container image.
3. Deploy the fulfillment service.
4. Run all test cases.
5. Clean up the kind cluster.

### Deployment mode

By default, the integration tests deploy the service using _Helm_. You can also deploy using
_Kustomize_ by setting the `IT_DEPLOY_MODE` environment variable:

```bash
$ IT_DEPLOY_MODE=kustomize ginkgo run it
```

Valid values are `helm` (default) and `kustomize`.

### Preserving the test cluster

By default, the kind cluster is deleted after the tests complete. If you want to preserve the cluster
for debugging or manual inspection, you can set the `IT_KEEP_KIND` environment variable:

```bash
$ IT_KEEP_KIND=true ginkgo run it
```

When `IT_KEEP_KIND=true`, the cluster will remain running after the tests finish, allowing you to:
- Inspect the deployed resources with `kubectl`.
- Debug test failures manually.
- Examine logs and cluster state.

The `setup` label can be combined with this to get a fresh integration environment where you can then
run your manual tests:

```bash
$ IT_KEEP_KIND=true ginkgo run --label-filter setup it
```

That will create the Kind cluster, install the dependencies and deploy the application, but will not
run any actual test.

To clean up a preserved cluster manually:

```bash
$ kind delete cluster --name fulfillment-service-it
```
