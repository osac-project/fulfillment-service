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

-- This migration hardens the hubs table. Hubs are infrastructure-level resources that must always belong to the shared
-- tenant, so any hub previously assigned to another tenant is reassigned. It also backfills the name column for hubs
-- that don't have one yet (copying the identifier), switches the primary key from 'id' to the composite (tenant, name),
-- keeps 'id' as a unique column, and attaches the immutable-columns trigger so that the id, name and tenant columns
-- cannot be changed after creation.

-- Move all hubs to the shared tenant:
update hubs set tenant = 'shared' where tenant != 'shared';

-- Backfill the name column for hubs that have an empty name:
update hubs set name = id where name = '';

-- Replace the primary key: drop the single-column key on 'id' and add a composite key on (tenant, name). The 'id'
-- column is preserved as unique so that lookups by identifier continue to work efficiently.
alter table hubs drop constraint hubs_pkey;
alter table hubs add primary key (tenant, name);
alter table hubs add constraint hubs_id_unique unique (id);

-- Attach the trigger to the hubs table to enforce immutability of the id, name and tenant columns:
create trigger check_immutable_columns
  before update on hubs
  for each row
  execute function check_immutable_columns('id', 'name', 'tenant');
