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

var _ = DescribeMigration("Create projects tables", func() {
	It("Creates the projects table", func() {
		// Run the migration:
		err := tool.Migrate(ctx, 45)
		Expect(err).ToNot(HaveOccurred())

		// Verify the table exists by inserting a row:
		_, err = pool.Exec(
			ctx,
			`insert into projects (id, data) values ('123', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates the archived projects table", func() {
		// Run the migration:
		err := tool.Migrate(ctx, 45)
		Expect(err).ToNot(HaveOccurred())

		// Verify the archived table exists by inserting a row:
		_, err = pool.Exec(
			ctx,
			`insert into archived_projects (id, creation_timestamp, deletion_timestamp, data)
			 values ('456', now(), now(), '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())
	})
})
