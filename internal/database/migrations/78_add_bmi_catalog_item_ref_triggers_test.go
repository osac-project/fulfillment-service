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
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Add BMI catalog item ref triggers", func() {
	It("Creates the 'check_bmi_catalog_item_not_in_use' function", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				information_schema.routines
			where
				routine_name = 'check_bmi_catalog_item_not_in_use' and
				routine_type = 'FUNCTION'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Adds a trigger to the bare_metal_instance_catalog_items table", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				information_schema.triggers
			where
				trigger_name = 'check_bmi_catalog_item_not_in_use' and
				event_object_table = 'bare_metal_instance_catalog_items' and
				action_timing = 'BEFORE' and
				event_manipulation = 'UPDATE'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Creates the 'check_bmi_catalog_item_ref' function", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				information_schema.routines
			where
				routine_name = 'check_bmi_catalog_item_ref' and
				routine_type = 'FUNCTION'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Adds a trigger to the bare_metal_instances table for catalog item ref on INSERT", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				information_schema.triggers
			where
				trigger_name = 'check_bmi_catalog_item_ref_on_insert' and
				event_object_table = 'bare_metal_instances' and
				action_timing = 'BEFORE' and
				event_manipulation = 'INSERT'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Adds a trigger to the bare_metal_instances table for catalog item ref on UPDATE", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		var count int
		err = conn.QueryRow(ctx, `
			select
				count(*)
			from
				information_schema.triggers
			where
				trigger_name = 'check_bmi_catalog_item_ref_on_update' and
				event_object_table = 'bare_metal_instances' and
				action_timing = 'BEFORE' and
				event_manipulation = 'UPDATE'
		`).Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("Prevents soft-deleting a BMI catalog item that is in use by a bare metal instance", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-1', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-1', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-1"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-1'`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0003"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-1"))
		Expect(pgErr.Message).To(ContainSubstring("bmi-1"))
	})

	It("Prevents soft-deleting a BMI catalog item when a bare metal instance references it by name", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, name, tenant, data)
			 values ('bmici-byname-1', 'my-bmici-name', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-byname-1', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"my-bmici-name"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-byname-1'`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0003"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-byname-1"))
		Expect(pgErr.Message).To(ContainSubstring("bmi-byname-1"))
	})

	It("Allows soft-deleting a BMI catalog item that is not in use", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-2', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-2'`)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Allows soft-deleting a tenant-scoped BMI catalog item when only a different-tenant bare metal instance references one with the same name", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('bmici-tenant-a', 'bmici-tenant-a', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())
		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('bmici-tenant-b', 'bmici-tenant-b', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-tenant-1a', 'bmici-tenant-a', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-tenant-1b', 'bmici-tenant-b', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-tenant-1', 'bmici-tenant-b', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-tenant-1b"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-tenant-1a'`)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Prevents soft-deleting a shared BMI catalog item when a bare metal instance in any tenant references it", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('bmici-tenant-c', 'bmici-tenant-c', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-global-1', 'shared', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-global-1', 'bmici-tenant-c', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-global-1"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-global-1'`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0003"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-global-1"))
		Expect(pgErr.Message).To(ContainSubstring("bmi-global-1"))
	})

	It("Allows soft-deleting a BMI catalog item when the referencing bare metal instance is already deleted", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-3', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, deletion_timestamp, data)
			 values ('bmi-2', 'test-tenant', now(), $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-3"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-3'`)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Prevents creating a bare metal instance referencing a non-existent BMI catalog item", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-ref-1', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"no-such-bmici"}}`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0002"))
		Expect(pgErr.Message).To(ContainSubstring("no-such-bmici"))
	})

	It("Prevents creating a bare metal instance referencing a soft-deleted BMI catalog item", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-deleted', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())
		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-deleted'`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-ref-2', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-deleted"}}`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0002"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-deleted"))
	})

	It("Allows creating a bare metal instance with no catalog item reference", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-ref-3', 'test-tenant', '{"spec":{}}')`)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Allows creating a bare metal instance with a valid catalog item reference", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-valid', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-ref-4', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-valid"}}`)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Allows creating a bare metal instance that references a shared BMI catalog item", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('bmici-tenant-d', 'bmici-tenant-d', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-global-ref-1', 'shared', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-global-ref-1', 'bmici-tenant-d', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-global-ref-1"}}`)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Prevents creating a bare metal instance that references a BMI catalog item in a different tenant", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('bmici-tenant-e', 'bmici-tenant-e', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())
		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('bmici-tenant-f', 'bmici-tenant-f', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-other-tenant-1', 'bmici-tenant-e', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-other-tenant-1', 'bmici-tenant-f', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-other-tenant-1"}}`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0002"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-other-tenant-1"))
	})

	It("Prevents updating a bare metal instance to reference a non-existent BMI catalog item", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-upd-1', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-upd-1', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-upd-1"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instances set data = $1::jsonb where id = 'bmi-upd-1'`,
			`{"spec":{"catalog_item":"no-such-bmici"}}`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0002"))
		Expect(pgErr.Message).To(ContainSubstring("no-such-bmici"))
	})

	It("Prevents updating a bare metal instance to reference a soft-deleted BMI catalog item", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-upd-2a', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())
		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-upd-2b', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-upd-2', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-upd-2a"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-upd-2b'`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instances set data = $1::jsonb where id = 'bmi-upd-2'`,
			`{"spec":{"catalog_item":"bmici-upd-2b"}}`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0002"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-upd-2b"))
	})

	It("Prevents updating a bare metal instance when its referenced catalog item has been soft-deleted", func(ctx context.Context) {
		err := tool.Migrate(ctx, 78)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into tenants (id, name, tenant, creator, data)
			 values ('test-tenant', 'test-tenant', 'system', 'system', '{}')
			 on conflict do nothing`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instance_catalog_items (id, tenant, data)
			 values ('bmici-upd-3', 'test-tenant', '{}')`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`insert into bare_metal_instances (id, tenant, data)
			 values ('bmi-upd-3', 'test-tenant', $1::jsonb)`,
			`{"spec":{"catalog_item":"bmici-upd-3"}}`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instances set data = '{"spec":{}}'::jsonb where id = 'bmi-upd-3'`)
		Expect(err).ToNot(HaveOccurred())
		_, err = conn.Exec(ctx,
			`update bare_metal_instance_catalog_items set deletion_timestamp = now() where id = 'bmici-upd-3'`)
		Expect(err).ToNot(HaveOccurred())

		_, err = conn.Exec(ctx,
			`update bare_metal_instances set data = $1::jsonb where id = 'bmi-upd-3'`,
			`{"spec":{"catalog_item":"bmici-upd-3"}}`)
		Expect(err).To(HaveOccurred())
		var pgErr *pgconn.PgError
		Expect(errors.As(err, &pgErr)).To(BeTrue())
		Expect(pgErr.Code).To(Equal("Z0002"))
		Expect(pgErr.Message).To(ContainSubstring("bmici-upd-3"))
	})
})
