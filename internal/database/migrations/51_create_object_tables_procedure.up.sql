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

-- This migration creates a reusable stored procedure that sets up the full database schema for a new object type. It
-- creates the object table with all standard DAO columns, the corresponding archive table, the standard set of indexes
-- for name, creator, tenant, and label queries, and the tenant foreign key constraint referencing the organizations
-- table.
--
-- Future migrations that introduce a new object type can call this procedure instead of repeating the boilerplate DDL.

create procedure create_object_schema(object_name text) language plpgsql as $$
begin
  -- Create the object table with all standard columns expected by the generic DAO:
  execute format(
    $ddl$
    create table %I (
      id text not null primary key,
      name text not null default '',
      creation_timestamp timestamp with time zone not null default now(),
      deletion_timestamp timestamp with time zone not null default 'epoch',
      finalizers text[] not null default '{}',
      creator text not null default '',
      tenant text not null default '',
      labels jsonb not null default '{}'::jsonb,
      annotations jsonb not null default '{}'::jsonb,
      data jsonb not null,
      version integer not null default 0
    )
    $ddl$,
    object_name
  );

  -- Create the archive table. It mirrors the object table but replaces the primary key with a plain column (multiple
  -- archived versions of the same object may coexist), drops the finalizers column (archived objects are no longer
  -- subject to finalization), removes defaults from 'creation_timestamp' and 'deletion_timestamp' (they are copied from
  -- the original row), and adds an 'archival_timestamp' column:
  execute format(
    $ddl$
    create table %I (
      id text not null,
      name text not null default '',
      creation_timestamp timestamp with time zone not null,
      deletion_timestamp timestamp with time zone not null,
      archival_timestamp timestamp with time zone not null default now(),
      creator text not null default '',
      tenant text not null default '',
      labels jsonb not null default '{}'::jsonb,
      annotations jsonb not null default '{}'::jsonb,
      data jsonb not null,
      version integer not null default 0
    )
    $ddl$,
    'archived_' || object_name
  );

  -- Create B-tree indexes for the columns used most frequently in queries:
  execute format('create index %I on %I (name)', object_name || '_by_name', object_name);
  execute format('create index %I on %I (creator)', object_name || '_by_creator', object_name);
  execute format('create index %I on %I (tenant)', object_name || '_by_tenant', object_name);

  -- Create a GIN index on the labels column for containment queries:
  execute format('create index %I on %I using gin (labels)', object_name || '_by_label', object_name);

  -- Add a foreign key constraint ensuring the tenant references an existing tenant:
  execute format(
    'alter table %I add constraint %I foreign key (tenant) references organizations (id)',
    object_name, object_name || '_tenant_fk'
  );
end;
$$;
