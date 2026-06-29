"""Minimal CEL-subset filter evaluator for List operations.

Supports:
  this.field.path == "value"
  this.field.path != "value"
  this.field.path.startsWith("prefix")
  this.field.path.endsWith("suffix")
  this.field.path.contains("substr")
  has(this.field.path)
  expr && expr
  expr || expr
"""

from __future__ import annotations

import re
from typing import Any, Callable


def parse_filter(expr: str) -> Callable[[dict], bool] | None:
    """Parse a CEL-like filter expression into a predicate function.

    Returns None if the expression is empty or unparseable (matches all).
    """
    expr = expr.strip()
    if not expr:
        return None
    try:
        return _parse_or(expr)
    except _ParseError:
        return None


class _ParseError(Exception):
    pass


def _parse_or(expr: str) -> Callable[[dict], bool]:
    parts = _split_top_level(expr, "||")
    if len(parts) == 1:
        return _parse_and(parts[0])
    fns = [_parse_and(p) for p in parts]
    return lambda obj: any(fn(obj) for fn in fns)


def _parse_and(expr: str) -> Callable[[dict], bool]:
    parts = _split_top_level(expr, "&&")
    if len(parts) == 1:
        return _parse_atom(parts[0])
    fns = [_parse_atom(p) for p in parts]
    return lambda obj: all(fn(obj) for fn in fns)


def _split_top_level(expr: str, op: str) -> list[str]:
    """Split on operator, respecting parentheses and string literals."""
    parts: list[str] = []
    depth = 0
    in_string = False
    string_char = ""
    current: list[str] = []
    i = 0
    while i < len(expr):
        c = expr[i]
        if in_string:
            current.append(c)
            if c == string_char:
                in_string = False
        elif c in ('"', "'"):
            in_string = True
            string_char = c
            current.append(c)
        elif c == "(":
            depth += 1
            current.append(c)
        elif c == ")":
            depth -= 1
            current.append(c)
        elif depth == 0 and expr[i : i + len(op)] == op:
            parts.append("".join(current).strip())
            current = []
            i += len(op)
            continue
        else:
            current.append(c)
        i += 1
    remainder = "".join(current).strip()
    if remainder:
        parts.append(remainder)
    return parts


_HAS_RE = re.compile(r'^has\(this\.(.+)\)$')
_COMPARE_RE = re.compile(r'^this\.(.+?)\s*(==|!=)\s*"(.*)"$')
_METHOD_RE = re.compile(r'^this\.(.+?)\.(startsWith|endsWith|contains)\("(.*)"\)$')
_NEGATION_RE = re.compile(r'^!\s*(.+)$')


def _parse_atom(expr: str) -> Callable[[dict], bool]:
    expr = expr.strip()

    if expr.startswith("(") and expr.endswith(")"):
        return _parse_or(expr[1:-1])

    m = _NEGATION_RE.match(expr)
    if m:
        inner = _parse_atom(m.group(1))
        return lambda obj, _f=inner: not _f(obj)

    m = _HAS_RE.match(expr)
    if m:
        path = m.group(1)
        return lambda obj, _p=path: _get_path(obj, _p) is not None

    m = _METHOD_RE.match(expr)
    if m:
        path, method, arg = m.group(1), m.group(2), m.group(3)
        return lambda obj, _p=path, _m=method, _a=arg: _string_method(
            _get_path(obj, _p), _m, _a
        )

    m = _COMPARE_RE.match(expr)
    if m:
        path, op, value = m.group(1), m.group(2), m.group(3)
        if op == "==":
            return lambda obj, _p=path, _v=value: _stringify(_get_path(obj, _p)) == _v
        else:
            return lambda obj, _p=path, _v=value: _stringify(_get_path(obj, _p)) != _v

    raise _ParseError(f"Unsupported expression: {expr}")


def _stringify(v: Any) -> str:
    if v is None:
        return ""
    if isinstance(v, bool):
        return "true" if v else "false"
    return str(v)


def _get_path(obj: dict, path: str) -> Any:
    """Navigate dotted path, handling map access like labels["key"]."""
    node: Any = obj
    for part in _split_path(path):
        if node is None:
            return None
        if isinstance(part, tuple):
            field, key = part
            node = node.get(field, {}) if isinstance(node, dict) else None
            node = node.get(key) if isinstance(node, dict) else None
        else:
            node = node.get(part) if isinstance(node, dict) else None
    return node


_MAP_ACCESS_RE = re.compile(r'^(.+)\["(.+)"\]$')


def _split_path(path: str) -> list[str | tuple[str, str]]:
    """Split 'metadata.labels["env"]' into [metadata, (labels, env)]."""
    parts: list[str | tuple[str, str]] = []
    for segment in path.split("."):
        m = _MAP_ACCESS_RE.match(segment)
        if m:
            parts.append((m.group(1), m.group(2)))
        else:
            parts.append(segment)
    return parts


def _string_method(value: Any, method: str, arg: str) -> bool:
    if value is None:
        return False
    s = str(value)
    if method == "startsWith":
        return s.startswith(arg)
    elif method == "endsWith":
        return s.endswith(arg)
    elif method == "contains":
        return arg in s
    return False
