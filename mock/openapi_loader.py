"""Parse the fulfillment-service OpenAPI v3 spec and discover resource types."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path

import yaml


@dataclass
class ResourceConfig:
    name: str  # e.g. "virtual_networks"
    collection_path: str  # e.g. "/api/fulfillment/v1/virtual_networks"
    item_path: str | None  # e.g. "/api/fulfillment/v1/virtual_networks/{id}"
    update_path: str | None  # e.g. "/api/fulfillment/v1/virtual_networks/{object.id}"
    schema_ref: str | None  # e.g. "v1VirtualNetwork"
    state_enum_values: list[str] = field(default_factory=list)
    has_create: bool = False
    has_update: bool = False
    has_delete: bool = False
    extra_paths: dict[str, list[str]] = field(default_factory=dict)


_COLLECTION_RE = re.compile(r"^/api/fulfillment/v1/([a-z_]+)$")
_ITEM_RE = re.compile(r"^/api/fulfillment/v1/([a-z_]+)/\{id\}$")
_UPDATE_RE = re.compile(r"^/api/fulfillment/v1/([a-z_]+)/\{object\.id\}$")
_EXTRA_RE = re.compile(r"^/api/fulfillment/v1/([a-z_]+)/\{id\}/(.+)$")


def _resolve_ref(spec: dict, ref: str) -> dict | None:
    if not ref.startswith("#/"):
        return None
    parts = ref.lstrip("#/").split("/")
    node = spec
    for p in parts:
        node = node.get(p, {})
    return node if node else None


def _find_state_enum(spec: dict, schema_ref: str) -> list[str]:
    """Given a resource schema ref name, find state enum values from status.state."""
    schema = spec.get("components", {}).get("schemas", {}).get(schema_ref, {})
    status_prop = schema.get("properties", {}).get("status", {})

    status_ref = status_prop.get("$ref", "")
    if status_ref:
        status_schema = _resolve_ref(spec, status_ref)
    else:
        status_schema = status_prop

    if not status_schema:
        return []

    state_prop = status_schema.get("properties", {}).get("state", {})
    state_ref = state_prop.get("$ref", "")
    if state_ref:
        state_schema = _resolve_ref(spec, state_ref)
    else:
        state_schema = state_prop

    if not state_schema:
        return []

    return state_schema.get("enum", [])


def _extract_schema_ref(spec: dict, path_info: dict) -> str | None:
    """Extract the resource schema ref from a List or Create response."""
    for method in ("get", "post"):
        method_info = path_info.get(method, {})
        resp_200 = method_info.get("responses", {}).get("200", {})
        content = resp_200.get("content", {}).get("application/json", {})
        schema = content.get("schema", {})
        ref = schema.get("$ref", "")
        if not ref:
            continue
        resp_schema = _resolve_ref(spec, ref)
        if not resp_schema:
            continue
        # List responses have items array, Create/Get responses have object
        for prop_name in ("items", "object"):
            prop = resp_schema.get("properties", {}).get(prop_name, {})
            if "$ref" in prop:
                return prop["$ref"].rsplit("/", 1)[-1]
            if prop.get("items", {}).get("$ref"):
                return prop["items"]["$ref"].rsplit("/", 1)[-1]
    return None


def discover_resources(spec: dict) -> list[ResourceConfig]:
    """Discover all CRUD resource types from the OpenAPI spec."""
    resources: dict[str, ResourceConfig] = {}
    paths = spec.get("paths", {})

    for path, path_info in paths.items():
        m = _COLLECTION_RE.match(path)
        if m:
            name = m.group(1)
            if name not in resources:
                resources[name] = ResourceConfig(
                    name=name,
                    collection_path=path,
                    item_path=None,
                    update_path=None,
                    schema_ref=None,
                )
            resources[name].collection_path = path
            resources[name].has_create = "post" in path_info
            resources[name].schema_ref = _extract_schema_ref(spec, path_info)
            continue

        m = _ITEM_RE.match(path)
        if m:
            name = m.group(1)
            if name not in resources:
                resources[name] = ResourceConfig(
                    name=name,
                    collection_path=f"/api/fulfillment/v1/{name}",
                    item_path=None,
                    update_path=None,
                    schema_ref=None,
                )
            resources[name].item_path = path
            resources[name].has_delete = "delete" in path_info
            continue

        m = _UPDATE_RE.match(path)
        if m:
            name = m.group(1)
            if name not in resources:
                resources[name] = ResourceConfig(
                    name=name,
                    collection_path=f"/api/fulfillment/v1/{name}",
                    item_path=None,
                    update_path=None,
                    schema_ref=None,
                )
            resources[name].update_path = path
            resources[name].has_update = "patch" in path_info
            continue

        m = _EXTRA_RE.match(path)
        if m:
            name = m.group(1)
            sub = m.group(2)
            if name in resources:
                methods = list(path_info.keys())
                resources[name].extra_paths[sub] = methods

    for rc in resources.values():
        if rc.schema_ref:
            rc.state_enum_values = _find_state_enum(spec, rc.schema_ref)

    return sorted(resources.values(), key=lambda r: r.name)


def load_spec(spec_path: str | Path) -> dict:
    with open(spec_path) as f:
        return yaml.safe_load(f)
