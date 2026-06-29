"""Per-resource state machine definitions and background ticker."""

from __future__ import annotations

import asyncio
import random
from datetime import datetime, timezone
from typing import Any

from store import ResourceStore

MOCK_LABEL_PREFIX = "mock.osac.dev/"


def _random_ip(base: str = "10.0.1") -> str:
    return f"{base}.{random.randint(2, 254)}"


def _populate_cluster_ready(obj: dict) -> dict:
    name = obj.get("metadata", {}).get("name", "cluster")
    return {
        "status": {
            "api_url": f"https://api.{name}.mock.osac.dev:6443",
            "console_url": f"https://console.{name}.mock.osac.dev",
        }
    }


def _populate_compute_instance_running(obj: dict) -> dict:
    return {
        "status": {
            "internal_ip_address": _random_ip(),
        }
    }


def _populate_public_ip_allocated(obj: dict) -> dict:
    pool = obj.get("spec", {}).get("pool", "")
    return {
        "status": {
            "address": f"203.0.113.{random.randint(1, 254)}",
            "pool": pool,
        }
    }


def _populate_external_ip_allocated(obj: dict) -> dict:
    pool = obj.get("spec", {}).get("pool", "")
    return {
        "status": {
            "address": f"198.51.100.{random.randint(1, 254)}",
            "pool": pool,
        }
    }


class Transition:
    def __init__(
        self,
        next_state: str,
        delay: float = 1.0,
        fail_state: str | None = None,
        on_ready: Any = None,
    ):
        self.next_state = next_state
        self.delay = delay
        self.fail_state = fail_state
        self.on_ready = on_ready


class StateMachineConfig:
    def __init__(
        self,
        state_field: str = "status.state",
        initial_state: str = "",
        transitions: dict[str, Transition] | None = None,
    ):
        self.state_field = state_field
        self.initial_state = initial_state
        self.transitions = transitions or {}


STATE_MACHINES: dict[str, StateMachineConfig] = {
    "virtual_networks": StateMachineConfig(
        initial_state="VIRTUAL_NETWORK_STATE_PENDING",
        transitions={
            "VIRTUAL_NETWORK_STATE_PENDING": Transition(
                next_state="VIRTUAL_NETWORK_STATE_READY",
                delay=1.0,
                fail_state="VIRTUAL_NETWORK_STATE_FAILED",
            ),
        },
    ),
    "subnets": StateMachineConfig(
        initial_state="SUBNET_STATE_PENDING",
        transitions={
            "SUBNET_STATE_PENDING": Transition(
                next_state="SUBNET_STATE_READY",
                delay=1.0,
                fail_state="SUBNET_STATE_FAILED",
            ),
        },
    ),
    "security_groups": StateMachineConfig(
        initial_state="SECURITY_GROUP_STATE_PENDING",
        transitions={
            "SECURITY_GROUP_STATE_PENDING": Transition(
                next_state="SECURITY_GROUP_STATE_READY",
                delay=0.5,
                fail_state="SECURITY_GROUP_STATE_FAILED",
            ),
        },
    ),
    "clusters": StateMachineConfig(
        initial_state="CLUSTER_STATE_PROGRESSING",
        transitions={
            "CLUSTER_STATE_PROGRESSING": Transition(
                next_state="CLUSTER_STATE_READY",
                delay=2.0,
                fail_state="CLUSTER_STATE_FAILED",
                on_ready=_populate_cluster_ready,
            ),
        },
    ),
    "compute_instances": StateMachineConfig(
        initial_state="COMPUTE_INSTANCE_STATE_STARTING",
        transitions={
            "COMPUTE_INSTANCE_STATE_STARTING": Transition(
                next_state="COMPUTE_INSTANCE_STATE_RUNNING",
                delay=1.5,
                fail_state="COMPUTE_INSTANCE_STATE_FAILED",
                on_ready=_populate_compute_instance_running,
            ),
        },
    ),
    "baremetal_instances": StateMachineConfig(
        initial_state="BARE_METAL_INSTANCE_STATE_PROVISIONING",
        transitions={
            "BARE_METAL_INSTANCE_STATE_PROVISIONING": Transition(
                next_state="BARE_METAL_INSTANCE_STATE_RUNNING",
                delay=3.0,
                fail_state="BARE_METAL_INSTANCE_STATE_FAILED",
            ),
        },
    ),
    "public_ips": StateMachineConfig(
        initial_state="PUBLIC_IP_STATE_PENDING",
        transitions={
            "PUBLIC_IP_STATE_PENDING": Transition(
                next_state="PUBLIC_IP_STATE_ALLOCATED",
                delay=1.0,
                fail_state="PUBLIC_IP_STATE_FAILED",
                on_ready=_populate_public_ip_allocated,
            ),
        },
    ),
    "public_ip_attachments": StateMachineConfig(
        initial_state="PUBLIC_IP_ATTACHMENT_STATE_PENDING",
        transitions={
            "PUBLIC_IP_ATTACHMENT_STATE_PENDING": Transition(
                next_state="PUBLIC_IP_ATTACHMENT_STATE_READY",
                delay=0.5,
                fail_state="PUBLIC_IP_ATTACHMENT_STATE_FAILED",
            ),
        },
    ),
    "external_ips": StateMachineConfig(
        initial_state="EXTERNAL_IP_STATE_PENDING",
        transitions={
            "EXTERNAL_IP_STATE_PENDING": Transition(
                next_state="EXTERNAL_IP_STATE_ALLOCATED",
                delay=1.0,
                fail_state="EXTERNAL_IP_STATE_FAILED",
                on_ready=_populate_external_ip_allocated,
            ),
        },
    ),
    "external_ip_attachments": StateMachineConfig(
        initial_state="EXTERNAL_IP_ATTACHMENT_STATE_PENDING",
        transitions={
            "EXTERNAL_IP_ATTACHMENT_STATE_PENDING": Transition(
                next_state="EXTERNAL_IP_ATTACHMENT_STATE_READY",
                delay=0.5,
                fail_state="EXTERNAL_IP_ATTACHMENT_STATE_FAILED",
            ),
        },
    ),
}


def _get_nested(obj: dict, path: str) -> Any:
    parts = path.split(".")
    node = obj
    for part in parts:
        if not isinstance(node, dict):
            return None
        node = node.get(part)
    return node


def _set_nested(obj: dict, path: str, value: Any) -> None:
    parts = path.split(".")
    node = obj
    for part in parts[:-1]:
        node = node.setdefault(part, {})
    node[parts[-1]] = value


def _deep_merge(base: dict, override: dict) -> None:
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(base.get(key), dict):
            _deep_merge(base[key], value)
        else:
            base[key] = value


async def run_ticker(store: ResourceStore, interval: float = 0.5) -> None:
    """Background task that advances resource state machines."""
    while True:
        await asyncio.sleep(interval)
        now = datetime.now(timezone.utc)

        for resource_type, config in STATE_MACHINES.items():
            resources = await store.all_resources(resource_type)
            for resource_id, obj in resources:
                current_state = _get_nested(obj, config.state_field)
                if current_state is None:
                    continue

                transition = config.transitions.get(current_state)
                if transition is None:
                    # Check for fail-after on terminal states
                    await _check_fail_after(store, resource_type, resource_id, obj, config, now)
                    continue

                entered_at_str = obj.get("_mock_entered_state_at")
                if not entered_at_str:
                    continue

                entered_at = datetime.fromisoformat(entered_at_str)
                elapsed = (now - entered_at).total_seconds()

                labels = obj.get("metadata", {}).get("labels", {})
                delay = transition.delay
                custom_delay = labels.get(f"{MOCK_LABEL_PREFIX}provision-delay")
                if custom_delay:
                    try:
                        delay = _parse_duration(custom_delay)
                    except ValueError:
                        pass

                if elapsed < delay:
                    continue

                should_fail = labels.get(f"{MOCK_LABEL_PREFIX}fail-provision") == "true"
                if should_fail and transition.fail_state:
                    error_msg = labels.get(
                        f"{MOCK_LABEL_PREFIX}error-message",
                        "Simulated provisioning failure",
                    )
                    updates: dict[str, Any] = {
                        "_mock_entered_state_at": now.isoformat(),
                    }
                    _set_nested(updates, config.state_field, transition.fail_state)
                    _set_nested(updates, "status.message", error_msg)
                    result = await store.update_internal(resource_type, resource_id, updates)
                    if result:
                        store._emit("OBJECT_UPDATED", resource_type, result)
                else:
                    updates = {
                        "_mock_entered_state_at": now.isoformat(),
                    }
                    _set_nested(updates, config.state_field, transition.next_state)
                    _set_nested(updates, "status.message", "")

                    if transition.on_ready:
                        extra = transition.on_ready(obj)
                        if extra:
                            _deep_merge(updates, extra)

                    result = await store.update_internal(
                        resource_type, resource_id, updates
                    )
                    if result:
                        store._emit("OBJECT_UPDATED", resource_type, result)


async def _check_fail_after(
    store: ResourceStore,
    resource_type: str,
    resource_id: str,
    obj: dict,
    config: StateMachineConfig,
    now: datetime,
) -> None:
    """Handle fail-after label: transition to FAILED after reaching a ready state."""
    labels = obj.get("metadata", {}).get("labels", {})
    fail_after_str = labels.get(f"{MOCK_LABEL_PREFIX}fail-after")
    if not fail_after_str:
        return

    entered_at_str = obj.get("_mock_entered_state_at")
    if not entered_at_str:
        return

    entered_at = datetime.fromisoformat(entered_at_str)
    elapsed = (now - entered_at).total_seconds()

    try:
        fail_after = _parse_duration(fail_after_str)
    except ValueError:
        return

    if elapsed < fail_after:
        return

    current_state = _get_nested(obj, config.state_field) or ""
    for transition in config.transitions.values():
        if transition.fail_state and current_state == transition.next_state:
            error_msg = labels.get(
                f"{MOCK_LABEL_PREFIX}error-message",
                "Simulated delayed failure",
            )
            updates: dict[str, Any] = {
                "_mock_entered_state_at": now.isoformat(),
            }
            _set_nested(updates, config.state_field, transition.fail_state)
            _set_nested(updates, "status.message", error_msg)
            new_labels = dict(labels)
            del new_labels[f"{MOCK_LABEL_PREFIX}fail-after"]
            updates.setdefault("metadata", {})["labels"] = new_labels
            result = await store.update_internal(resource_type, resource_id, updates)
            if result:
                store._emit("OBJECT_UPDATED", resource_type, result)
            return


def _parse_duration(s: str) -> float:
    """Parse a duration string like '3s', '1.5s', '500ms'."""
    s = s.strip()
    if s.endswith("ms"):
        return float(s[:-2]) / 1000
    if s.endswith("s"):
        return float(s[:-1])
    if s.endswith("m"):
        return float(s[:-1]) * 60
    return float(s)
