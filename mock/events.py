"""SSE event stream — emits events when resources change state."""

from __future__ import annotations

import asyncio
import json
from datetime import datetime, timezone

from sse_starlette.sse import EventSourceResponse
from starlette.requests import Request


class EventBus:
    """Fan-out event bus for SSE clients."""

    def __init__(self) -> None:
        self._subscribers: list[asyncio.Queue[dict]] = []
        self._lock = asyncio.Lock()

    async def subscribe(self) -> asyncio.Queue[dict]:
        queue: asyncio.Queue[dict] = asyncio.Queue(maxsize=256)
        async with self._lock:
            self._subscribers.append(queue)
        return queue

    async def unsubscribe(self, queue: asyncio.Queue[dict]) -> None:
        async with self._lock:
            try:
                self._subscribers.remove(queue)
            except ValueError:
                pass

    def publish(
        self,
        event_type: str,
        resource_type: str,
        resource_id: str,
        obj: dict,
    ) -> None:
        event = _build_event(event_type, resource_type, resource_id, obj)
        for queue in list(self._subscribers):
            try:
                queue.put_nowait(event)
            except asyncio.QueueFull:
                pass


_EVENT_TYPE_MAP = {
    "OBJECT_CREATED": "EVENT_TYPE_OBJECT_CREATED",
    "OBJECT_UPDATED": "EVENT_TYPE_OBJECT_UPDATED",
    "OBJECT_DELETED": "EVENT_TYPE_OBJECT_DELETED",
}

_RESOURCE_TYPE_MAP: dict[str, str] = {}


def _resource_type_to_field(resource_type: str) -> str:
    """Convert 'virtual_networks' to 'virtual_network' (singular form for event field)."""
    if resource_type in _RESOURCE_TYPE_MAP:
        return _RESOURCE_TYPE_MAP[resource_type]
    singular = resource_type.rstrip("s")
    if resource_type.endswith("ies"):
        singular = resource_type[:-3] + "y"
    elif resource_type.endswith("sses"):
        singular = resource_type[:-2]
    elif resource_type.endswith("ses"):
        singular = resource_type[:-2]
    _RESOURCE_TYPE_MAP[resource_type] = singular
    return singular


def _strip_internal(obj: dict) -> dict:
    return {k: v for k, v in obj.items() if not k.startswith("_mock_")}


def _build_event(
    event_type: str,
    resource_type: str,
    resource_id: str,
    obj: dict,
) -> dict:
    field_name = _resource_type_to_field(resource_type)
    return {
        "type": _EVENT_TYPE_MAP.get(event_type, event_type),
        "object_type": resource_type,
        "object_id": resource_id,
        "timestamp": datetime.now(timezone.utc).isoformat(),
        field_name: _strip_internal(obj),
    }


async def events_endpoint(request: Request, event_bus: EventBus) -> EventSourceResponse:
    tenant = getattr(request.state, "tenant", "") or ""
    is_admin = "cloud-provider-admin" in (getattr(request.state, "roles", None) or [])
    queue = await event_bus.subscribe()

    async def event_generator():
        try:
            while True:
                if await request.is_disconnected():
                    break
                try:
                    event = await asyncio.wait_for(queue.get(), timeout=30.0)
                    if not is_admin and tenant:
                        obj_tenant = ""
                        for key in event:
                            if isinstance(event[key], dict):
                                obj_tenant = (
                                    event[key].get("metadata", {}).get("tenant", "")
                                )
                                break
                        if obj_tenant and obj_tenant != tenant:
                            continue
                    yield {"data": json.dumps(event)}
                except asyncio.TimeoutError:
                    yield {"comment": "keepalive"}
        finally:
            await event_bus.unsubscribe(queue)

    return EventSourceResponse(event_generator())
