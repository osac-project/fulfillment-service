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

var _ = DescribeMigration("Create object tables procedure", func() {
	It("Creates the 'create_object_schema' procedure", func(ctx context.Context) {
		err := tool.Migrate(ctx, 51)
		Expect(err).ToNot(HaveOccurred())

		var count int
		row := conn.QueryRow(ctx, `
			select
				count(*)
			from
				pg_catalog.pg_proc p
			join
				pg_catalog.pg_namespace n on n.oid = p.pronamespace
			where
				n.nspname = 'public' and
				p.proname = 'create_object_schema' and
				p.prokind = 'p'
		`)
		err = row.Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})
})
