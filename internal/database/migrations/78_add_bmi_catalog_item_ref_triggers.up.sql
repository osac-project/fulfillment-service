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

-- This migration adds bidirectional referential integrity triggers between bare metal instance catalog items and bare
-- metal instances, mirroring the triggers added in migration 59 for clusters and compute instances:
--
-- 1. BEFORE UPDATE trigger on bare_metal_instance_catalog_items that prevents soft-deleting a catalog item while active
--    bare metal instances still reference it.
--
-- 2. BEFORE INSERT OR UPDATE triggers on bare_metal_instances that prevent creating or updating a resource that
--    references a catalog item that does not exist or has been soft-deleted. These triggers use FOR SHARE to acquire a
--    shared lock on the catalog item row, which conflicts with the exclusive lock held by a concurrent catalog item
--    soft-delete. This bidirectional locking eliminates the TOCTOU race condition.

-- Trigger function that checks whether any active bare metal instance references the bare metal instance catalog item
-- being deleted:
create function check_bmi_catalog_item_not_in_use() returns trigger as $$
declare
  child_id text;
begin
  select id into child_id
  from bare_metal_instances
  where deletion_timestamp = 'epoch'
    and (data->'spec'->>'catalog_item' = old.id or data->'spec'->>'catalog_item' = old.name)
    and (old.tenant = 'shared' or bare_metal_instances.tenant = old.tenant)
  limit 1;

  if child_id is not null then
    raise exception using
      errcode = 'Z0003',
      message = format(
        'cannot delete bare metal instance catalog item ''%s'': it is in use by at least bare metal instance ''%s''',
        old.id, child_id
      );
  end if;

  return new;
end;
$$ language plpgsql;

-- Attach the trigger so that it only fires when the row transitions from active to soft-deleted:
create trigger check_bmi_catalog_item_not_in_use
  before update on bare_metal_instance_catalog_items
  for each row
  when (old.deletion_timestamp = 'epoch' and new.deletion_timestamp != 'epoch')
  execute function check_bmi_catalog_item_not_in_use();

-- Trigger function that validates the bare metal instance catalog item reference when creating or updating a bare
-- metal instance:
create function check_bmi_catalog_item_ref() returns trigger as $$
declare
  catalog_item_ref text;
  found_id text;
begin
  catalog_item_ref := new.data->'spec'->>'catalog_item';
  if coalesce(catalog_item_ref, '') != '' then
    select id into found_id
    from bare_metal_instance_catalog_items
    where (id = catalog_item_ref or name = catalog_item_ref)
      and deletion_timestamp = 'epoch'
      and (tenant = new.tenant or tenant = 'shared')
    for share;

    if found_id is null then
      raise exception using
        errcode = 'Z0002',
        message = format(
          'bare metal instance catalog item ''%s'' does not exist or has been deleted',
          catalog_item_ref
        );
    end if;
  end if;

  return new;
end;
$$ language plpgsql;

-- Attach the trigger so that it fires on insert of active bare metal instances:
create trigger check_bmi_catalog_item_ref_on_insert
  before insert on bare_metal_instances
  for each row
  when (new.deletion_timestamp = 'epoch')
  execute function check_bmi_catalog_item_ref();

-- Attach the trigger so that it fires on update of active bare metal instances, but only when the
-- catalog item reference actually changes (avoids a FOR SHARE lock on every status update):
create trigger check_bmi_catalog_item_ref_on_update
  before update on bare_metal_instances
  for each row
  when (new.deletion_timestamp = 'epoch' and
    (new.data->'spec'->>'catalog_item') is distinct from (old.data->'spec'->>'catalog_item'))
  execute function check_bmi_catalog_item_ref();
