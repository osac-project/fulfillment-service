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

-- This migration adds a 'project' column to all resource tables and their archived counterparts.

-- Helper function to add the project column to a table:
create or replace function add_project_column(table_name text) returns void as $$
begin
  execute format('alter table %I add column project text not null default ''''', table_name);
end;
$$ language plpgsql;

-- Helper function to create an index on the project column:
create or replace function add_project_index(table_name text) returns void as $$
begin
  execute format('create index %I on %I (project)', table_name || '_by_project', table_name);
end;
$$ language plpgsql;

-- Add the project column to all main tables:
select add_project_column('cluster_templates');
select add_project_column('clusters');
select add_project_column('hubs');
select add_project_column('compute_instance_templates');
select add_project_column('compute_instances');
select add_project_column('host_types');
select add_project_column('network_classes');
select add_project_column('virtual_networks');
select add_project_column('subnets');
select add_project_column('security_groups');
select add_project_column('public_ip_pools');
select add_project_column('public_ips');
select add_project_column('organizations');
select add_project_column('users');
select add_project_column('roles');
select add_project_column('role_bindings');
select add_project_column('cluster_catalog_items');
select add_project_column('compute_instance_catalog_items');
select add_project_column('public_ip_attachments');

-- Add the project column to all archived tables:
select add_project_column('archived_cluster_templates');
select add_project_column('archived_clusters');
select add_project_column('archived_hubs');
select add_project_column('archived_compute_instance_templates');
select add_project_column('archived_compute_instances');
select add_project_column('archived_host_types');
select add_project_column('archived_network_classes');
select add_project_column('archived_virtual_networks');
select add_project_column('archived_subnets');
select add_project_column('archived_security_groups');
select add_project_column('archived_public_ip_pools');
select add_project_column('archived_public_ips');
select add_project_column('archived_organizations');
select add_project_column('archived_users');
select add_project_column('archived_roles');
select add_project_column('archived_role_bindings');
select add_project_column('archived_cluster_catalog_items');
select add_project_column('archived_compute_instance_catalog_items');
select add_project_column('archived_public_ip_attachments');

-- Create indexes on the project column for main tables:
select add_project_index('cluster_templates');
select add_project_index('clusters');
select add_project_index('hubs');
select add_project_index('compute_instance_templates');
select add_project_index('compute_instances');
select add_project_index('host_types');
select add_project_index('network_classes');
select add_project_index('virtual_networks');
select add_project_index('subnets');
select add_project_index('security_groups');
select add_project_index('public_ip_pools');
select add_project_index('public_ips');
select add_project_index('organizations');
select add_project_index('users');
select add_project_index('roles');
select add_project_index('role_bindings');
select add_project_index('cluster_catalog_items');
select add_project_index('compute_instance_catalog_items');
select add_project_index('public_ip_attachments');

-- Drop the helper functions:
drop function add_project_column;
drop function add_project_index;
