"""In-memory resource store with CRUD operations and tenant scoping."""

from __future__ import annotations

import asyncio
import copy
import uuid
from datetime import datetime, timezone
from typing import Any, Callable


class ConflictError(Exception):
    pass


class NotFoundError(Exception):
    pass


class BadRequestError(Exception):
    def __init__(self, message: str = "Bad request"):
        self.message = message
        super().__init__(message)


class ResourceStore:
    def __init__(self) -> None:
        self._data: dict[str, dict[str, dict]] = {}
        self._lock = asyncio.Lock()
        self._event_callback: Callable[[str, str, str, dict], None] | None = None

    def set_event_callback(
        self, cb: Callable[[str, str, str, dict], None]
    ) -> None:
        """Set callback(event_type, resource_type, resource_id, obj) for state changes."""
        self._event_callback = cb

    def _emit(self, event_type: str, resource_type: str, obj: dict) -> None:
        if self._event_callback:
            self._event_callback(event_type, resource_type, obj.get("id", ""), obj)

    async def list(
        self,
        resource_type: str,
        tenant: str | None = None,
        filter_fn: Callable[[dict], bool] | None = None,
        offset: int = 0,
        limit: int | None = None,
    ) -> tuple[list[dict], int]:
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            items = list(bucket.values())

        if tenant and tenant != "*":
            items = [
                i
                for i in items
                if i.get("metadata", {}).get("tenant", "") in (tenant, "", None)
            ]

        if filter_fn:
            items = [i for i in items if filter_fn(i)]

        total = len(items)
        if offset:
            items = items[offset:]
        if limit is not None and limit > 0:
            items = items[:limit]

        return [_strip_internal(i) for i in items], total

    async def get(
        self, resource_type: str, resource_id: str, tenant: str | None = None
    ) -> dict:
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            obj = bucket.get(resource_id)

        if obj is None:
            raise NotFoundError()

        if tenant and tenant != "*":
            obj_tenant = obj.get("metadata", {}).get("tenant", "")
            if obj_tenant and obj_tenant != tenant:
                raise NotFoundError()

        return _strip_internal(obj)

    async def create(
        self,
        resource_type: str,
        obj: dict,
        tenant: str = "",
        creator: str = "",
        initial_state: str | None = None,
        state_field: str = "status.state",
    ) -> dict:
        resource_id = obj.get("id") or str(uuid.uuid4())
        now = datetime.now(timezone.utc).isoformat()

        metadata = obj.get("metadata", {})
        metadata.setdefault("creation_timestamp", now)
        metadata.setdefault("version", 1)
        metadata.setdefault("labels", {})
        metadata.setdefault("annotations", {})
        if tenant:
            metadata["tenant"] = tenant
        if creator:
            metadata["creator"] = creator
        obj["metadata"] = metadata
        obj["id"] = resource_id

        if initial_state:
            _set_nested(obj, state_field, initial_state)
            obj["_mock_entered_state_at"] = now

        async with self._lock:
            bucket = self._data.setdefault(resource_type, {})
            if resource_id in bucket:
                raise ConflictError()
            bucket[resource_id] = copy.deepcopy(obj)

        self._emit("OBJECT_CREATED", resource_type, obj)
        return _strip_internal(obj)

    async def update(
        self,
        resource_type: str,
        resource_id: str,
        updates: dict,
        lock: bool = False,
        tenant: str | None = None,
    ) -> dict:
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            obj = bucket.get(resource_id)
            if obj is None:
                raise NotFoundError()

            if tenant and tenant != "*":
                obj_tenant = obj.get("metadata", {}).get("tenant", "")
                if obj_tenant and obj_tenant != tenant:
                    raise NotFoundError()

            current_version = obj.get("metadata", {}).get("version", 0)
            if lock:
                incoming_version = updates.get("metadata", {}).get("version")
                if incoming_version is None or incoming_version != current_version:
                    raise ConflictError()

            updates = copy.deepcopy(updates)
            update_meta = updates.get("metadata", {})
            for protected in ("version", "tenant", "creator", "creation_timestamp"):
                update_meta.pop(protected, None)
            if update_meta:
                updates["metadata"] = update_meta
            elif "metadata" in updates:
                del updates["metadata"]

            _deep_merge(obj, updates)
            obj["metadata"]["version"] = current_version + 1
            bucket[resource_id] = obj
            result = copy.deepcopy(obj)

        self._emit("OBJECT_UPDATED", resource_type, result)
        return _strip_internal(result)

    async def delete(
        self,
        resource_type: str,
        resource_id: str,
        tenant: str | None = None,
    ) -> bool:
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            obj = bucket.get(resource_id)
            if obj is None:
                raise NotFoundError()

            if tenant and tenant != "*":
                obj_tenant = obj.get("metadata", {}).get("tenant", "")
                if obj_tenant and obj_tenant != tenant:
                    raise NotFoundError()

            deleted = bucket.pop(resource_id, None)

        if deleted:
            self._emit("OBJECT_DELETED", resource_type, deleted)
        return deleted is not None

    async def update_internal(
        self, resource_type: str, resource_id: str, updates: dict
    ) -> dict | None:
        """Update without version bump or tenant check — used by state machine."""
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            obj = bucket.get(resource_id)
            if obj is None:
                return None
            _deep_merge(obj, updates)
            return copy.deepcopy(obj)

    async def get_internal(self, resource_type: str, resource_id: str) -> dict | None:
        """Get raw object including internal fields."""
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            obj = bucket.get(resource_id)
            return copy.deepcopy(obj) if obj else None

    async def all_resources(
        self, resource_type: str
    ) -> list[tuple[str, dict]]:
        """Return all (id, obj) pairs for a resource type, including internal fields."""
        async with self._lock:
            bucket = self._data.get(resource_type, {})
            return [(k, copy.deepcopy(v)) for k, v in bucket.items()]

    async def insert_raw(self, resource_type: str, obj: dict) -> None:
        """Insert a pre-built object directly — used by scenario loader."""
        resource_id = obj.get("id") or str(uuid.uuid4())
        obj["id"] = resource_id
        async with self._lock:
            bucket = self._data.setdefault(resource_type, {})
            bucket[resource_id] = copy.deepcopy(obj)


def _strip_internal(obj: dict) -> dict:
    result = copy.deepcopy(obj)
    for key in list(result.keys()):
        if key.startswith("_mock_"):
            del result[key]
    return result


def _deep_merge(base: dict, override: dict) -> None:
    for key, value in override.items():
        if key == "id":
            continue
        if (
            isinstance(value, dict)
            and isinstance(base.get(key), dict)
            and key != "labels"
            and key != "annotations"
        ):
            _deep_merge(base[key], value)
        else:
            base[key] = copy.deepcopy(value)


def _set_nested(obj: dict, path: str, value: Any) -> None:
    parts = path.split(".")
    node = obj
    for part in parts[:-1]:
        node = node.setdefault(part, {})
    node[parts[-1]] = value


def _get_nested(obj: dict, path: str) -> Any:
    parts = path.split(".")
    node = obj
    for part in parts:
        if not isinstance(node, dict):
            return None
        node = node.get(part)
    return node
