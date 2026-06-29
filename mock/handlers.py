"""Generic CRUD request handlers for all resource types."""

from __future__ import annotations

from fastapi import Request, Response
from fastapi.responses import JSONResponse

from filter_parser import parse_filter
from store import BadRequestError, ConflictError, NotFoundError, ResourceStore

MOCK_LABEL_PREFIX = "mock.osac.dev/"


def get_tenant(request: Request) -> str:
    return getattr(request.state, "tenant", "") or ""


def get_user(request: Request) -> str:
    return getattr(request.state, "user", "") or "anonymous"


def get_roles(request: Request) -> list[str]:
    return getattr(request.state, "roles", [])


def is_admin(request: Request) -> bool:
    return "cloud-provider-admin" in get_roles(request)


async def handle_list(
    resource_type: str,
    store: ResourceStore,
    request: Request,
    initial_state: str | None = None,
) -> JSONResponse:
    tenant = None if is_admin(request) else get_tenant(request)

    def _to_int(value: str | None) -> int | None:
        try:
            return max(0, int(value)) if value is not None else None
        except ValueError:
            return None

    offset = _to_int(request.query_params.get("offset")) or 0
    limit = _to_int(request.query_params.get("limit"))
    filter_expr = request.query_params.get("filter", "")
    filter_fn = parse_filter(filter_expr)

    items, total = await store.list(
        resource_type, tenant=tenant, filter_fn=filter_fn, offset=offset, limit=limit
    )
    return JSONResponse(
        {"items": items, "size": len(items), "total": total}
    )


async def handle_get(
    resource_type: str,
    resource_id: str,
    store: ResourceStore,
    request: Request,
) -> JSONResponse:
    tenant = None if is_admin(request) else get_tenant(request)
    try:
        obj = await store.get(resource_type, resource_id, tenant=tenant)
    except NotFoundError:
        return JSONResponse(
            {"code": 5, "message": "Not found", "details": []}, status_code=404
        )
    return JSONResponse({"object": obj})


async def handle_create(
    resource_type: str,
    store: ResourceStore,
    request: Request,
    initial_state: str | None = None,
    state_field: str = "status.state",
) -> JSONResponse:
    try:
        body = await request.json()
    except Exception:
        return JSONResponse(
            {"code": 3, "message": "Invalid request body", "details": []},
            status_code=400,
        )
    obj = body.get("object", body)

    labels = (obj.get("metadata") or {}).get("labels") or {}
    if labels.get(f"{MOCK_LABEL_PREFIX}fail-create") == "true":
        msg = labels.get(f"{MOCK_LABEL_PREFIX}error-message", "Simulated create failure")
        return JSONResponse(
            {"code": 3, "message": msg, "details": []}, status_code=400
        )

    tenant = get_tenant(request)
    creator = get_user(request)

    try:
        created = await store.create(
            resource_type,
            obj,
            tenant=tenant,
            creator=creator,
            initial_state=initial_state,
            state_field=state_field,
        )
    except ConflictError:
        return JSONResponse(
            {"code": 6, "message": "Already exists", "details": []}, status_code=409
        )
    except BadRequestError as e:
        return JSONResponse(
            {"code": 3, "message": e.message, "details": []}, status_code=400
        )
    return JSONResponse({"object": created})


async def handle_update(
    resource_type: str,
    resource_id: str,
    store: ResourceStore,
    request: Request,
) -> JSONResponse:
    body = await request.json()
    lock = request.query_params.get("lock", "").lower() == "true"

    updates = body.get("object", body)

    tenant = None if is_admin(request) else get_tenant(request)
    try:
        updated = await store.update(
            resource_type, resource_id, updates, lock=lock, tenant=tenant
        )
    except NotFoundError:
        return JSONResponse(
            {"code": 5, "message": "Not found", "details": []}, status_code=404
        )
    except ConflictError:
        return JSONResponse(
            {"code": 9, "message": "Version conflict", "details": []}, status_code=409
        )
    return JSONResponse({"object": updated})


async def handle_delete(
    resource_type: str,
    resource_id: str,
    store: ResourceStore,
    request: Request,
) -> Response:
    tenant = None if is_admin(request) else get_tenant(request)

    try:
        obj = await store.get(resource_type, resource_id, tenant=tenant)
    except NotFoundError:
        return JSONResponse(
            {"code": 5, "message": "Not found", "details": []}, status_code=404
        )

    labels = (obj.get("metadata") or {}).get("labels") or {}
    if labels.get(f"{MOCK_LABEL_PREFIX}fail-delete") == "true":
        msg = labels.get(f"{MOCK_LABEL_PREFIX}error-message", "Simulated delete failure")
        return JSONResponse(
            {"code": 13, "message": msg, "details": []}, status_code=500
        )

    try:
        await store.delete(resource_type, resource_id, tenant=tenant)
    except NotFoundError:
        return JSONResponse(
            {"code": 5, "message": "Not found", "details": []}, status_code=404
        )
    return JSONResponse({})
