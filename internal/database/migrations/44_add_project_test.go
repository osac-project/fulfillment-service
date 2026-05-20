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
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Add project column", func() {
	It("Adds project column with empty default to clusters table", func() {
		// Insert a row before the migration:
		_, err := pool.Exec(
			ctx,
			`insert into clusters (id, data) values ('123', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 44)
		Expect(err).ToNot(HaveOccurred())

		// Verify the project column exists and has the expected default:
		var actual string
		row := pool.QueryRow(
			ctx,
			`select project from clusters where id = '123'`,
		)
		err = row.Scan(&actual)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(""))
	})

	It("Adds project column with empty default to archived clusters table", func() {
		// Run the migration:
		err := tool.Migrate(ctx, 44)
		Expect(err).ToNot(HaveOccurred())

		// Verify the project column exists by inserting a row with an explicit value:
		_, err = pool.Exec(
			ctx,
			`insert into archived_clusters (
				id, creation_timestamp, deletion_timestamp, data
			) values (
				'456', now(), now(), '{}'
			)`,
		)
		Expect(err).ToNot(HaveOccurred())

		var actual string
		row := pool.QueryRow(
			ctx,
			`select project from archived_clusters where id = '456'`,
		)
		err = row.Scan(&actual)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(""))
	})
})
