--
-- Copyright (c) 2026 Red Hat Inc.
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
  'ocp_small',
  'shared',
  '{
    "id": "ocp_small",
    "title": "OpenShift Small Cluster",
    "description": "OpenShift cluster with configurable OCP version and small instances as worker nodes. Default OCP version: 4.22.0.",
    "parameters": [
      {
        "type": "type.googleapis.com/google.protobuf.StringValue",
        "name": "ocp_release_version",
        "title": "OCP Release Version",
        "description": "The OCP version to deploy.",
        "default": {
          "@type": "type.googleapis.com/google.protobuf.StringValue",
          "value": "4.22.0"
        }
      }
    ],
    "specDefaults": {
      "releaseImage": "quay.io/openshift-release-dev/ocp-release:4.22.0-multi"
    }
  }'
);
