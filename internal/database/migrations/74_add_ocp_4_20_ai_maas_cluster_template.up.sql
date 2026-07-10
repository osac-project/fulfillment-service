--
-- Copyright (c) 2025 Red Hat Inc.
--
-- Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with
-- the License. You may obtain a copy of the License at
--
--   http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on
-- an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
-- specific language governing permissions and limitations under the License.
--

insert into cluster_templates (id, tenant, data) values
(
  'ocp_4_20_ai_maas',
  'shared',
  '{
    "id": "ocp_4_20_ai_maas",
    "title": "OpenShift AI 4.20 Cluster with MaaS",
    "description": "Builds an OpenShift 4.20 cluster with RHOAI 3.4, Models-as-a-Service (GA), Keycloak SSO, and observability.",
    "parameters": [
      {
        "type": "type.googleapis.com/google.protobuf.BoolValue",
        "name": "enable_maas",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.BoolValue",
          "value": true
        }
      },
      {
        "type": "type.googleapis.com/google.protobuf.BoolValue",
        "name": "enable_keycloak",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.BoolValue",
          "value": true
        }
      },
      {
        "type": "type.googleapis.com/google.protobuf.StringValue",
        "name": "oidc_issuer",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.StringValue",
          "value": ""
        }
      },
      {
        "type": "type.googleapis.com/google.protobuf.StringValue",
        "name": "oidc_client_id",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.StringValue",
          "value": ""
        }
      },
      {
        "type": "type.googleapis.com/google.protobuf.BoolValue",
        "name": "enable_observability",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.BoolValue",
          "value": true
        }
      },
      {
        "type": "type.googleapis.com/google.protobuf.BoolValue",
        "name": "enable_dashboard",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.BoolValue",
          "value": true
        }
      }
    ]
  }'
) on conflict (id) do nothing;
