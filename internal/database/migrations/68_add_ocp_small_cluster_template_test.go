/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package migrations

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Add ocp_small cluster template", func() {
	It("Inserts the ocp_small cluster template", func(ctx context.Context) {
		err := tool.Migrate(ctx, 68)
		Expect(err).ToNot(HaveOccurred())

		var data []byte
		row := conn.QueryRow(ctx,
			`select data from cluster_templates where id = 'ocp_small'`,
		)
		err = row.Scan(&data)
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(MatchJSON(`{
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
		}`))
	})

	It("Assigns the template to the shared tenant", func(ctx context.Context) {
		err := tool.Migrate(ctx, 68)
		Expect(err).ToNot(HaveOccurred())

		var tenant string
		row := conn.QueryRow(ctx,
			`select tenant from cluster_templates where id = 'ocp_small'`,
		)
		err = row.Scan(&tenant)
		Expect(err).ToNot(HaveOccurred())
		Expect(tenant).To(Equal("shared"))
	})
})
