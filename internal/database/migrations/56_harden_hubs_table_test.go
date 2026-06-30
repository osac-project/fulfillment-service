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

	"github.com/osac-project/fulfillment-service/internal/auth"
)

var _ = DescribeMigration("Harden hubs table", func() {
	It("Moves all hubs to the shared tenant", func(ctx context.Context) {
		// Create an additional tenant so the foreign key constraint is satisfied:
		_, err := conn.Exec(ctx, `
			insert into organizations (id, name, tenant, creator, data)
			values ('my-tenant', 'my-tenant', 'my-tenant', 'shared', '{}')
		`)
		Expect(err).ToNot(HaveOccurred())

		// Insert a hub with a non-shared tenant before the migration:
		_, err = conn.Exec(ctx,
			`insert into hubs (id, tenant, data) values ('my-hub', 'my-tenant', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// Verify that the hub now belongs to the shared tenant:
		var tenant string
		err = conn.QueryRow(ctx, `select tenant from hubs where id = 'my-hub'`).Scan(&tenant)
		Expect(err).ToNot(HaveOccurred())
		Expect(tenant).To(Equal(auth.SharedTenant))
	})

	It("Copies the identifier to the name for hubs with an empty name", func(ctx context.Context) {
		// Insert a hub without a name (defaults to empty string):
		_, err := conn.Exec(ctx,
			`insert into hubs (id, tenant, data) values ('hub-no-name', 'shared', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Insert a hub that already has a name:
		_, err = conn.Exec(ctx,
			`insert into hubs (id, name, tenant, data) values ('hub-with-name', 'my-hub', 'shared', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// The hub that had no name should now use its id as the name:
		var name string
		err = conn.QueryRow(ctx, `select name from hubs where id = 'hub-no-name'`).Scan(&name)
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal("hub-no-name"))

		// The hub that already had a name should keep it:
		err = conn.QueryRow(ctx, `select name from hubs where id = 'hub-with-name'`).Scan(&name)
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal("my-hub"))
	})

	It("Creates a composite primary key on (tenant, name)", func(ctx context.Context) {
		// Insert a hub before the migration:
		_, err := conn.Exec(ctx,
			`insert into hubs (id, name, tenant, data) values ('hub-1', 'hub-1', 'shared', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// Verify the primary key is on (tenant, name):
		var columns []string
		rows, err := conn.Query(ctx, `
			select
				a.attname
			from
				pg_constraint con
			join
				pg_class c on c.oid = con.conrelid
			join
				pg_attribute a on a.attrelid = c.oid and a.attnum = any(con.conkey)
			where
				c.relname = 'hubs' and
				con.contype = 'p'
		`)
		Expect(err).ToNot(HaveOccurred())
		defer rows.Close()
		for rows.Next() {
			var col string
			err = rows.Scan(&col)
			Expect(err).ToNot(HaveOccurred())
			columns = append(columns, col)
		}
		Expect(rows.Err()).ToNot(HaveOccurred())
		Expect(columns).To(ConsistOf("tenant", "name"))
	})

	It("Makes the id column unique but not a primary key", func(ctx context.Context) {
		err := tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// Verify the unique constraint exists on id:
		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				pg_constraint con
			join
				pg_class c on c.oid = con.conrelid
			join
				pg_attribute a on a.attrelid = c.oid and a.attnum = any(con.conkey)
			where
				c.relname = 'hubs' and
				con.contype = 'u' and
				a.attname = 'id'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Adds an immutable-columns trigger to the hubs table", func(ctx context.Context) {
		err := tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				pg_trigger t
			join
				pg_class c on c.oid = t.tgrelid
			where
				t.tgname = 'check_immutable_columns' and
				c.relname = 'hubs'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Rejects updates to the tenant column", func(ctx context.Context) {
		// Insert a hub before the migration:
		_, err := conn.Exec(ctx,
			`insert into hubs (id, tenant, data) values ('hub-immutable', 'shared', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// Attempt to change the tenant, which should be rejected by the trigger:
		_, err = conn.Exec(ctx,
			`update hubs set tenant = 'system' where id = 'hub-immutable'`,
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("immutable"))
	})

	It("Rejects duplicate (tenant, name) pairs", func(ctx context.Context) {
		// Insert a hub before the migration:
		_, err := conn.Exec(ctx,
			`insert into hubs (id, name, tenant, data) values ('hub-1', 'my-hub', 'shared', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Run the migration:
		err = tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// Try to insert another hub with the same tenant and name but different id:
		_, err = conn.Exec(ctx,
			`insert into hubs (id, name, tenant, data) values ('hub-2', 'my-hub', 'shared', '{}')`,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Rejects duplicate id values", func(ctx context.Context) {
		err := tool.Migrate(ctx, 56)
		Expect(err).ToNot(HaveOccurred())

		// Insert a hub:
		_, err = conn.Exec(ctx,
			`insert into hubs (id, name, tenant, data) values ('hub-1', 'hub-a', 'shared', '{}')`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Try to insert another hub with the same id but a different name:
		_, err = conn.Exec(ctx,
			`insert into hubs (id, name, tenant, data) values ('hub-1', 'hub-b', 'shared', '{}')`,
		)
		Expect(err).To(HaveOccurred())
	})
})
