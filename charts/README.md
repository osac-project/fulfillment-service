# Kubernetes deployment

This directory contains the Helm charts used to deploy the service to a Kubernetes cluster.

The main chart is `service`, which can be configured for either OpenShift (intended for production environments) or
Kind (intended for development and testing environments) using the `variant` value.

## OpenShift

The gRPC protocol is based on HTTP2, which isn't enabled by default in OpenShift. To enable it run this command:

```shell
$ oc annotate ingresses.config/cluster ingress.operator.openshift.io/default-enable-http2=true
```

Install the _cert-manager_ operator:

```shell
$ oc new-project cert-manager-operator

$ oc create -f - <<.
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  namespace: cert-manager-operator
  name: cert-manager-operator
spec:
  upgradeStrategy: Default
.

$ oc create -f - <<.
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: openshift-operators
  name: cert-manager
spec:
  channel: stable
  installPlanApproval: Automatic
  name: cert-manager
  source: community-operators
  sourceNamespace: openshift-marketplace
.
```

Install the _trust-manager_ operator:

```shell
$ helm install trust-manager oci://quay.io/jetstack/charts/trust-manager \
--version v0.20.0 \
--namespace cert-manager-operator \
--set app.trust.namespace=cert-manager \
--set defaultPackage.enabled=false \
--wait
```

Create the default CA:

```shell
$ helm install default-ca charts/ca \
--namespace cert-manager
```

Install the _Authorino_ operator:

```shell
$ oc create -f - <<.
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: openshift-operators
  name: authorino-operator
spec:
  name: authorino-operator
  sourceNamespace: openshift-marketplace
  source: redhat-operators
  channel: stable
  installPlanApproval: Automatic
.
```

Deploy the application:

```shell
$ helm install fulfillment-service charts/service \
--namespace osac \
--create-namespace \
--set variant=openshift \
--set certs.issuerRef.name=default-ca \
--set certs.caBundle.configMap=ca-bundle \
--set auth.issuerUrl=https://your-oauth-issuer-url
```

## Kind

To create the Kind cluster use a command like this:

```yaml
$ kind create cluster --config - <<.
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
name: osac
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 8000
    listenAddress: "0.0.0.0"
.
```

The cluster uses a single port mapping: external port 8000 on the host is forwarded to internal port 30000 in the
cluster. This port is used by the Envoy Gateway for ingress traffic.

Install the _cert-manager_ operator:

```shell
$ helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
--version v1.19.1 \
--namespace cert-manager \
--create-namespace \
--set crds.enabled=true \
--wait
```

Install the _trust-manager_ operator:

```shell
$ helm install trust-manager oci://quay.io/jetstack/charts/trust-manager \
--version v0.20.0 \
--namespace cert-manager \
--set defaultPackage.enabled=false \
--wait
```

Create the default CA:

```shell
$ helm install default-ca charts/ca \
--namespace cert-manager
```

Install the _Envoy Gateway_ that provides the Gateway API implementation used for routing traffic to the services:

```shell
$ helm install envoy-gateway oci://docker.io/envoyproxy/gateway-helm \
--version v1.6.1 \
--namespace envoy-gateway \
--create-namespace \
--wait
```

Create the default gateway configuration. First, create an `EnvoyProxy` resource that configures the gateway service
to use a `NodePort` with port 30000 (the internal ingress port mapped from the host):

```shell
$ kubectl apply -f - <<.
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyProxy
metadata:
  namespace: envoy-gateway
  name: default
spec:
  provider:
    type: Kubernetes
    kubernetes:
      envoyService:
        type: NodePort
        patch:
          type: StrategicMerge
          value:
            spec:
              ports:
              - name: https
                port: 443
                nodePort: 30000
.
```

Create the default `GatewayClass` that references the `EnvoyProxy` configuration:

```shell
$ kubectl apply -f - <<.
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: default
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
  parametersRef:
    group: gateway.envoyproxy.io
    kind: EnvoyProxy
    namespace: envoy-gateway
    name: default
.
```

Create the default `Gateway` with a TLS passthrough listener:

```shell
$ kubectl apply -f - <<.
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  namespace: envoy-gateway
  name: default
spec:
  gatewayClassName: default
  listeners:
  - name: tls
    protocol: TLS
    port: 443
    tls:
      mode: Passthrough
    allowedRoutes:
      namespaces:
        from: All
.
```

Install the _Authorino_ operator. Due to an issue with the configuration of custom CA certificates in the Authorino
operator (see [here](https://github.com/Kuadrant/authorino-operator/pull/282)) it is necessary to replace the operator
image. So you need to download the manifests, replace the image, and then apply the result:

```shell
$ curl -o authorino.yaml https://raw.githubusercontent.com/Kuadrant/authorino-operator/refs/heads/release-v0.22.0/config/deploy/manifests.yaml
$ sed -i 's|quay.io/kuadrant/authorino-operator:v0.20.0|quay.io/osac/authorino-operator:latest|g' authorino.yaml
$ kubectl apply -f authorino.yaml
```

Deploy the application:

```shell
$ helm install fulfillment-service charts/service \
--namespace osac \
--create-namespace \
--set variant=kind \
--set certs.issuerRef.name=default-ca \
--set certs.caBundle.configMap=ca-bundle \
--set auth.issuerUrl=https://your-oauth-issuer-url
```
