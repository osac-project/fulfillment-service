/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dao

import (
	"context"
	"fmt"

	"github.com/osac-project/fulfillment-service/internal/database"
)

// CreateTables creates the tables, indexes, and archived tables for the provided object names. It gets the current
// transaction from the context and uses it to run the SQL statements.
func CreateTables(ctx context.Context, objects ...string) error {
	// Get the transaction from the context:
	tx, err := database.TxFromContext(ctx)
	if err != nil {
		return err
	}
	defer tx.ReportError(&err)

	// Create tables for each object:
	for _, object := range objects {
		err = createTable(ctx, tx, object)
		if err != nil {
			return err
		}
	}

	return nil
}

func createTable(ctx context.Context, tx database.Tx, object string) error {
	err := createMainTable(ctx, tx, object)
	if err != nil {
		return fmt.Errorf("failed to create table for object '%s': %w", object, err)
	}
	err = createArchivedTable(ctx, tx, object)
	if err != nil {
		return fmt.Errorf("failed to create archive table for object '%s': %w", object, err)
	}
	err = createIndexes(ctx, tx, object)
	if err != nil {
		return fmt.Errorf("failed to create indexes for object '%s': %w", object, err)
	}
	return nil
}

func createMainTable(ctx context.Context, tx database.Tx, object string) error {
	sql := fmt.Sprintf(
		`
		create table if not exists %s (
			id text not null primary key,
			name text not null default '',
			creation_timestamp timestamp with time zone not null default now(),
			deletion_timestamp timestamp with time zone not null default 'epoch',
			finalizers text[] not null default '{}',
			creators text[] not null default '{}',
			tenants text[] not null default '{}',
			labels jsonb not null default '{}'::jsonb,
			annotations jsonb not null default '{}'::jsonb,
			data jsonb not null
		)
		`,
		object,
	)
	_, err := tx.Exec(ctx, sql)
	return err
}

func createArchivedTable(ctx context.Context, tx database.Tx, object string) error {
	sql := fmt.Sprintf(
		`
		create table if not exists archived_%s (
			id text not null,
			name text not null default '',
			creation_timestamp timestamp with time zone not null,
			deletion_timestamp timestamp with time zone not null,
			archival_timestamp timestamp with time zone not null default now(),
			creators text[] not null default '{}',
			tenants text[] not null default '{}',
			labels jsonb not null default '{}'::jsonb,
			annotations jsonb not null default '{}'::jsonb,
			data jsonb not null
		)
		`,
		object,
	)
	_, err := tx.Exec(ctx, sql)
	return err
}

func createIndexes(ctx context.Context, tx database.Tx, object string) error {
	indexes := []string{
		"create index if not exists %[1]s_by_name on %[1]s (name)",
		"create index if not exists %[1]s_by_owner on %[1]s using gin (creators)",
		"create index if not exists %[1]s_by_tenant on %[1]s using gin (tenants)",
		"create index if not exists %[1]s_by_label on %[1]s using gin (labels)",
	}
	for _, format := range indexes {
		definition := fmt.Sprintf(format, object)
		_, err := tx.Exec(ctx, definition)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}
	return nil
}
