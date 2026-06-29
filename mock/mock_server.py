"""Fulfillment-service mock server.

Reads the OpenAPI v3 spec, discovers all resource types, and serves them
with in-memory state, JWT auth, state machine progression, and SSE events.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from contextlib import asynccontextmanager
from pathlib import Path

import uvicorn
import yaml
from fastapi import FastAPI, Request, Response
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse

from auth import AuthMiddleware, AuthProvider, USERS
from events import EventBus, events_endpoint
from handlers import (
    handle_create,
    handle_delete,
    handle_get,
    handle_list,
    handle_update,
)
from openapi_loader import ResourceConfig, discover_resources, load_spec
from state_machines import STATE_MACHINES, run_ticker
from store import ResourceStore

DEFAULT_SPEC = Path(__file__).parent.parent / "pages" / "openapi" / "v3" / "public.yaml"


async def load_scenario(store: ResourceStore, scenario_path: Path) -> int:
    """Load a YAML scenario file into the store. Returns count of loaded resources."""
    with open(scenario_path) as f:
        data = yaml.safe_load(f)

    resources = data.get("resources", [])
    count = 0

    for res in resources:
        resource_type = res.pop("type", None)
        if not resource_type:
            continue

        res.setdefault("metadata", {})
        res["metadata"].setdefault("labels", {})
        res["metadata"].setdefault("annotations", {})
        res["metadata"].setdefault("version", 1)

        state = (res.get("status") or {}).get("state")
        sm = STATE_MACHINES.get(resource_type)
        if state and sm:
            terminal_states = set()
            for t in sm.transitions.values():
                terminal_states.add(t.next_state)
                if t.fail_state:
                    terminal_states.add(t.fail_state)
            if state not in terminal_states:
                from datetime import datetime, timezone
                res["_mock_entered_state_at"] = datetime.now(timezone.utc).isoformat()

        await store.insert_raw(resource_type, res)
        count += 1

    return count


def create_app(
    spec_path: Path = DEFAULT_SPEC,
    scenario_paths: list[Path] | None = None,
    no_auth: bool = False,
    port: int = 8000,
) -> FastAPI:
    store = ResourceStore()
    event_bus = EventBus()

    store.set_event_callback(
        lambda et, rt, rid, obj: event_bus.publish(et, rt, rid, obj)
    )

    spec = load_spec(spec_path)
    resource_configs = discover_resources(spec)

    ticker_task: asyncio.Task | None = None

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        nonlocal ticker_task
        if scenario_paths:
            for sp in scenario_paths:
                count = await load_scenario(store, sp)
                print(f"Loaded {count} resources from {sp}")
        ticker_task = asyncio.create_task(run_ticker(store))
        print(f"State machine ticker started")
        yield
        if ticker_task:
            ticker_task.cancel()
            try:
                await ticker_task
            except asyncio.CancelledError:
                pass

    host = f"http://localhost:{port}"
    auth_provider = AuthProvider(issuer=host)

    app = FastAPI(
        title="Fulfillment Service Mock",
        version="0.1.0",
        lifespan=lifespan,
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.add_middleware(AuthMiddleware, auth_provider=auth_provider, no_auth=no_auth)

    # --- Auth endpoints ---

    @app.get("/.well-known/openid-configuration")
    async def openid_config():
        return auth_provider.openid_configuration()

    @app.get("/.well-known/jwks.json")
    async def jwks():
        return auth_provider.jwks()

    @app.post("/auth/token")
    async def token(request: Request):
        content_type = request.headers.get("content-type", "")
        if "application/json" in content_type:
            body = await request.json()
        else:
            body = dict(await request.form())

        username = body.get("username", "")
        password = body.get("password", "")
        organization = body.get("organization")

        user = USERS.get(username)
        if not user or user.password != password:
            return JSONResponse(
                {"error": "invalid_grant", "error_description": "Invalid credentials"},
                status_code=401,
            )

        token = auth_provider.create_token(username, organization)
        return {
            "access_token": token,
            "token_type": "bearer",
            "expires_in": 3600,
        }

    @app.get("/api/fulfillment/v1/capabilities")
    async def capabilities():
        return auth_provider.capabilities()

    # --- Events endpoint ---

    @app.get("/api/events/v1/events")
    async def events(request: Request):
        return await events_endpoint(request, event_bus)

    # --- Special cluster endpoints ---

    @app.get("/api/fulfillment/v1/clusters/{cluster_id}/kubeconfig")
    async def cluster_kubeconfig(cluster_id: str, request: Request):
        try:
            obj = await store.get("clusters", cluster_id)
        except Exception:
            return JSONResponse({"code": 5, "message": "Not found"}, status_code=404)
        name = obj.get("metadata", {}).get("name", "cluster")
        kubeconfig = f"""\
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://api.{name}.mock.osac.dev:6443
    certificate-authority-data: bW9jay1jYS1kYXRh
  name: {name}
contexts:
- context:
    cluster: {name}
    user: admin
  name: {name}
current-context: {name}
users:
- name: admin
  user:
    token: mock-admin-token
"""
        return Response(content=kubeconfig, media_type="application/yaml")

    @app.get("/api/fulfillment/v1/clusters/{cluster_id}/password")
    async def cluster_password(cluster_id: str, request: Request):
        try:
            await store.get("clusters", cluster_id)
        except Exception:
            return JSONResponse({"code": 5, "message": "Not found"}, status_code=404)
        return Response(content="mock-admin-password", media_type="text/plain")

    # --- Dynamic CRUD routes ---

    _register_crud_routes(app, resource_configs, store)

    print(f"Registered {len(resource_configs)} resource types:")
    for rc in resource_configs:
        sm = " [stateful]" if rc.name in STATE_MACHINES else ""
        print(f"  {rc.collection_path}{sm}")

    return app


def _register_crud_routes(
    app: FastAPI,
    resource_configs: list[ResourceConfig],
    store: ResourceStore,
) -> None:
    for rc in resource_configs:
        resource_type = rc.name
        sm = STATE_MACHINES.get(resource_type)

        initial_state = sm.initial_state if sm else None
        state_field = sm.state_field if sm else "status.state"

        _rt = resource_type
        _is = initial_state
        _sf = state_field

        app.get(rc.collection_path)(
            _make_list_handler(_rt, store)
        )

        if rc.has_create:
            app.post(rc.collection_path)(
                _make_create_handler(_rt, store, _is, _sf)
            )

        if rc.item_path:
            item_path = rc.item_path.replace("{id}", "{resource_id}")
            app.get(item_path)(
                _make_get_handler(_rt, store)
            )

            if rc.has_delete:
                app.delete(item_path)(
                    _make_delete_handler(_rt, store)
                )

        if rc.update_path and rc.has_update:
            update_path = rc.update_path.replace("{object.id}", "{resource_id}")
            app.patch(update_path)(
                _make_update_handler(_rt, store)
            )


def _make_list_handler(resource_type: str, store: ResourceStore):
    async def handler(request: Request):
        return await handle_list(resource_type, store, request)
    handler.__name__ = f"list_{resource_type}"
    return handler


def _make_get_handler(resource_type: str, store: ResourceStore):
    async def handler(resource_id: str, request: Request):
        return await handle_get(resource_type, resource_id, store, request)
    handler.__name__ = f"get_{resource_type}"
    return handler


def _make_create_handler(
    resource_type: str,
    store: ResourceStore,
    initial_state: str | None,
    state_field: str,
):
    async def handler(request: Request):
        return await handle_create(resource_type, store, request, initial_state, state_field)
    handler.__name__ = f"create_{resource_type}"
    return handler


def _make_update_handler(resource_type: str, store: ResourceStore):
    async def handler(resource_id: str, request: Request):
        return await handle_update(resource_type, resource_id, store, request)
    handler.__name__ = f"update_{resource_type}"
    return handler


def _make_delete_handler(resource_type: str, store: ResourceStore):
    async def handler(resource_id: str, request: Request):
        return await handle_delete(resource_type, resource_id, store, request)
    handler.__name__ = f"delete_{resource_type}"
    return handler


def main():
    parser = argparse.ArgumentParser(description="Fulfillment Service Mock Server")
    parser.add_argument(
        "--port", type=int, default=8000, help="Port to listen on (default: 8000)"
    )
    parser.add_argument(
        "--spec",
        type=Path,
        default=DEFAULT_SPEC,
        help="Path to OpenAPI v3 spec (default: ../pages/openapi/v3/public.yaml)",
    )
    parser.add_argument(
        "--scenario",
        type=Path,
        action="append",
        dest="scenarios",
        help="Path to scenario YAML file(s) to preload (can specify multiple)",
    )
    parser.add_argument(
        "--no-auth",
        action="store_true",
        help="Disable JWT authentication (all requests treated as admin)",
    )
    args = parser.parse_args()

    app = create_app(
        spec_path=args.spec,
        scenario_paths=args.scenarios,
        no_auth=args.no_auth,
        port=args.port,
    )
    uvicorn.run(app, host="0.0.0.0", port=args.port)


if __name__ == "__main__":
    main()
