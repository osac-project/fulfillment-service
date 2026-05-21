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
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Migrate network fields", func() {
	DescribeTable(
		"Migrates subnet and security_groups to network_attachments",
		func(inputData string, expectedData string) {
			// Insert a compute instance with the old format:
			_, err := pool.Exec(
				ctx,
				`insert into compute_instances (id, tenant, data) values ('test-ci-123', 'test-tenant', $1::jsonb)`,
				inputData,
			)
			Expect(err).ToNot(HaveOccurred())

			// Run the migration:
			err = tool.Migrate(ctx, 43)
			Expect(err).ToNot(HaveOccurred())

			// Fetch the migrated data:
			var actualData []byte
			row := pool.QueryRow(
				ctx,
				`select data from compute_instances where id = 'test-ci-123'`,
			)
			err = row.Scan(&actualData)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualData).To(MatchJSON(expectedData))
		},
		Entry(
			"Instance with only subnet",
			`{
				"spec": {
					"subnet": "subnet-123",
					"template": "template-1"
				}
			}`,
			`{
				"spec": {
					"template": "template-1",
					"network_attachments": [
						{
							"subnet": "subnet-123",
							"security_groups": []
						}
					]
				}
			}`,
		),
		Entry(
			"Instance with subnet and security groups",
			`{
				"spec": {
					"subnet": "subnet-456",
					"security_groups": ["sg-1", "sg-2"],
					"template": "template-1"
				}
			}`,
			`{
				"spec": {
					"template": "template-1",
					"network_attachments": [
						{
							"subnet": "subnet-456",
							"security_groups": ["sg-1", "sg-2"]
						}
					]
				}
			}`,
		),
		Entry(
			"Instance with only security groups (no subnet)",
			`{
				"spec": {
					"security_groups": ["sg-3"],
					"template": "template-1"
				}
			}`,
			`{
				"spec": {
					"template": "template-1",
					"network_attachments": [
						{
							"subnet": null,
							"security_groups": ["sg-3"]
						}
					]
				}
			}`,
		),
		Entry(
			"Instance already migrated (has network_attachments) - should not change",
			`{
				"spec": {
					"network_attachments": [
						{
							"subnet": "subnet-789",
							"security_groups": ["sg-4"]
						}
					],
					"template": "template-1"
				}
			}`,
			`{
				"spec": {
					"network_attachments": [
						{
							"subnet": "subnet-789",
							"security_groups": ["sg-4"]
						}
					],
					"template": "template-1"
				}
			}`,
		),
		Entry(
			"Instance with no networking fields - should not change",
			`{
				"spec": {
					"template": "template-1"
				}
			}`,
			`{
				"spec": {
					"template": "template-1"
				}
			}`,
		),
	)

	It("Migrates archived_compute_instances table", func() {
		// Insert an archived compute instance with old format:
		_, err := pool.Exec(
			ctx,
			`insert into archived_compute_instances (id, tenant, data, creation_timestamp, deletion_timestamp) values ('archived-ci-123', 'test-tenant', $1::jsonb, now(), now())`,
			`{
				"spec": {
					"subnet": "archived-subnet",
					"security_groups": ["archived-sg"],
					"template": "template-1"
				}
			}`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 43)
		Expect(err).ToNot(HaveOccurred())

		// Verify the archived instance was migrated:
		var actualData []byte
		row := pool.QueryRow(
			ctx,
			`select data from archived_compute_instances where id = 'archived-ci-123'`,
		)
		err = row.Scan(&actualData)
		Expect(err).ToNot(HaveOccurred())

		expectedData := `{
			"spec": {
				"template": "template-1",
				"network_attachments": [
					{
						"subnet": "archived-subnet",
						"security_groups": ["archived-sg"]
					}
				]
			}
		}`
		Expect(actualData).To(MatchJSON(expectedData))
	})
})
