{{/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with
the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
*/}}

{{/*
Generate the hostname for the fulfillment API. If .Values.externalHostname is set, use it; otherwise, use the default
Kubernetes service hostname based on the release namespace.
*/}}
{{- define "fulfillment-api.hostname" -}}
{{- if .Values.externalHostname -}}
{{- .Values.externalHostname -}}
{{- else -}}
{{- printf "fulfillment-api.%s.svc.cluster.local" .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{/*
Generate the token issuer URL for JWT signing and JWKS discovery.

On OpenShift with an external hostname, a Route provides TLS passthrough on
port 443, so the URL omits the port. In all other cases (kind variant, or no
external hostname) traffic goes directly to the Kubernetes Service on port 8000.
*/}}
{{- define "fulfillment-api.tokenIssuerUrl" -}}
{{- if and (eq .Values.variant "openshift") .Values.externalHostname -}}
https://{{ .Values.externalHostname }}
{{- else -}}
https://{{ include "fulfillment-api.hostname" . }}:8000
{{- end -}}
{{- end -}}

{{/*
Generate the hostname for the fulfillment internal API. If .Values.internalHostname is set, use it; otherwise, use the
default Kubernetes service hostname based on the release namespace.
*/}}
{{- define "fulfillment-internal-api.hostname" -}}
{{- if .Values.internalHostname -}}
{{- .Values.internalHostname -}}
{{- else -}}
{{- printf "fulfillment-internal-api.%s.svc.cluster.local" .Release.Namespace -}}
{{- end -}}
{{- end -}}
