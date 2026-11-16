"""OTLP log and span requests encoded straight to protobuf bytes.

The OpenTelemetry exporters build a protobuf message object for every record,
attribute and value, then serialise the lot. On free-threaded Python (3.14t)
protobuf has no C implementation and does that in pure Python, which made
encoding most of what an export costs. Writing the wire format directly skips
the objects, and the bytes for what repeats — the resource, the scope,
attribute keys, common string values — are made once.

The output is the same message the exporters send; tests decode one with
protobuf and compare it with theirs.
"""

import struct
from collections.abc import Mapping, Sequence

from opentelemetry.exporter.otlp.proto.common._internal import (
    _encode_instrumentation_scope,
    _encode_resource,
)

_pack_q = struct.Struct("<Q").pack
_pack_d = struct.Struct("<d").pack
_pack_i = struct.Struct("<I").pack
_SMALL = [bytes([i]) for i in range(128)]


def _varint(n: int) -> bytes:
    if 0 <= n < 128:
        return _SMALL[n]
    if n < 0:
        n += 1 << 64
    out = bytearray()
    while n > 127:
        out.append((n & 127) | 128)
        n >>= 7
    out.append(n)
    return bytes(out)


def _field(tag: bytes, payload: bytes) -> bytes:
    """A length-delimited field: tag, length, payload."""
    return tag + _varint(len(payload)) + payload


# Encoded attribute keys and short string values, by text: a program logs the
# same keys, and often the same values, over and over. Bounded, so a stream of
# distinct values cannot grow it without limit.
_KEYS: dict[str, bytes] = {}
_STRINGS: dict[str, bytes] = {}
_CACHE_LIMIT = 4096


def _key(k: str) -> bytes:
    b = _KEYS.get(k)
    if b is None:
        b = _field(b"\x0a", k.encode())
        if len(_KEYS) < _CACHE_LIMIT:
            _KEYS[k] = b
    return b


def _any_value(v) -> bytes:
    """An AnyValue message's bytes."""
    t = type(v)
    if t is str:
        b = _STRINGS.get(v)
        if b is None:
            b = _field(b"\x0a", v.encode())
            if len(v) <= 64 and len(_STRINGS) < _CACHE_LIMIT:
                _STRINGS[v] = b
        return b
    if t is bool:
        return b"\x10\x01" if v else b"\x10\x00"
    if t is int:
        return b"\x18" + _varint(v)
    if t is float:
        return b"\x21" + _pack_d(v)
    if t is bytes:
        return _field(b"\x3a", v)
    if isinstance(v, Mapping):
        return _field(
            b"\x32", b"".join(_field(b"\x0a", _kv(k, x)) for k, x in v.items())
        )
    if isinstance(v, Sequence):
        return _field(b"\x2a", b"".join(_field(b"\x0a", _any_value(x)) for x in v))
    if v is None:
        return b""
    raise TypeError(f"unsupported attribute value {type(v).__name__}")


def _kv(k: str, v) -> bytes:
    """A KeyValue message's bytes."""
    return _key(k) + _field(b"\x12", _any_value(v))


def _attributes(tag: bytes, attrs) -> bytes:
    if not attrs:
        return b""
    return b"".join(_field(tag, _kv(k, v)) for k, v in attrs.items())


# Resource and scope bytes, made once per object; the object is kept with
# them so its id cannot be reused by another.
_RESOURCES: dict[int, tuple[object, bytes]] = {}
_SCOPES: dict[int, tuple[object, bytes]] = {}


def _cached(cache: dict, obj, encode) -> bytes:
    hit = cache.get(id(obj))
    if hit is not None and hit[0] is obj:
        return hit[1]
    b = encode(obj).SerializeToString()
    if len(cache) < _CACHE_LIMIT:
        cache[id(obj)] = (obj, b)
    return b


def _group(batch, resource_of, scope_of):
    """Records grouped by resource, then scope, in first-seen order."""
    groups: dict[int, tuple[object, dict[int, tuple[object, list]]]] = {}
    for item in batch:
        resource, scope = resource_of(item), scope_of(item)
        scopes = groups.setdefault(id(resource), (resource, {}))[1]
        scopes.setdefault(id(scope), (scope, []))[1].append(item)
    return groups.values()


def _request(batch, resource_of, scope_of, encode_one) -> bytes:
    """An Export*ServiceRequest: resources holding scopes holding records.

    Logs and spans nest the same way with the same field numbers:
    Resource* {1 resource, 2 scope_*, 3 schema_url}, Scope* {1 scope,
    2 records, 3 schema_url}.
    """
    out = []
    for resource, scopes in _group(batch, resource_of, scope_of):
        rparts = [_field(b"\x0a", _cached(_RESOURCES, resource, _encode_resource))]
        for scope, items in scopes.values():
            sparts = []
            if scope is not None:
                sparts.append(
                    _field(
                        b"\x0a", _cached(_SCOPES, scope, _encode_instrumentation_scope)
                    )
                )
            sparts.extend(_field(b"\x12", encode_one(i)) for i in items)
            if scope is not None and scope.schema_url:
                sparts.append(_field(b"\x1a", scope.schema_url.encode()))
            rparts.append(_field(b"\x12", b"".join(sparts)))
        if resource.schema_url:
            rparts.append(_field(b"\x1a", resource.schema_url.encode()))
        out.append(_field(b"\x0a", b"".join(rparts)))
    return b"".join(out)


def _log_record(readable) -> bytes:
    """A logs.v1.LogRecord's bytes."""
    r = readable.log_record
    parts = []
    if r.timestamp:
        parts.append(b"\x09" + _pack_q(r.timestamp))
    sev = getattr(r.severity_number, "value", None)
    if sev:
        parts.append(b"\x10" + _varint(sev))
    if r.severity_text:
        parts.append(_field(b"\x1a", r.severity_text.encode()))
    if r.body is not None:
        parts.append(_field(b"\x2a", _any_value(r.body)))
    parts.append(_attributes(b"\x32", r.attributes))
    if readable.dropped_attributes:
        parts.append(b"\x38" + _varint(readable.dropped_attributes))
    if int(r.trace_flags or 0):
        parts.append(b"\x45" + _pack_i(int(r.trace_flags)))
    if r.trace_id:
        parts.append(b"\x4a\x10" + r.trace_id.to_bytes(16, "big"))
    if r.span_id:
        parts.append(b"\x52\x08" + r.span_id.to_bytes(8, "big"))
    if r.observed_timestamp:
        parts.append(b"\x59" + _pack_q(r.observed_timestamp))
    if getattr(r, "event_name", None):
        parts.append(_field(b"\x62", r.event_name.encode()))
    return b"".join(parts)


def encode_logs(batch) -> bytes:
    """An ExportLogsServiceRequest's bytes for a batch of ReadableLogRecords."""
    return _request(
        batch,
        lambda r: r.resource,
        lambda r: r.instrumentation_scope or None,
        _log_record,
    )


_HAS_IS_REMOTE = 0x100
_IS_REMOTE = 0x200


def _flags(context) -> bytes:
    """The span-flags field: whether the parent's remoteness is known, and is."""
    flags = _HAS_IS_REMOTE | (
        _IS_REMOTE if context is not None and context.is_remote else 0
    )
    return b"\x85\x01" + _pack_i(flags)


def _trace_state(ts) -> bytes:
    if not ts:
        return b""
    return _field(b"\x1a", ",".join(f"{k}={v}" for k, v in ts.items()).encode())


def _span(span) -> bytes:
    """A trace.v1.Span's bytes."""
    ctx = span.get_span_context()
    parts = [
        b"\x0a\x10" + ctx.trace_id.to_bytes(16, "big"),
        b"\x12\x08" + ctx.span_id.to_bytes(8, "big"),
        _trace_state(ctx.trace_state),
    ]
    if span.parent is not None:
        parts.append(b"\x22\x08" + span.parent.span_id.to_bytes(8, "big"))
    if span.name:
        parts.append(_field(b"\x2a", span.name.encode()))
    parts.append(b"\x30" + _varint(span.kind.value + 1))  # SPAN_KIND_INTERNAL is 1
    if span.start_time:
        parts.append(b"\x39" + _pack_q(span.start_time))
    if span.end_time:
        parts.append(b"\x41" + _pack_q(span.end_time))
    parts.append(_attributes(b"\x4a", span.attributes))
    if span.dropped_attributes:
        parts.append(b"\x50" + _varint(span.dropped_attributes))
    for e in span.events:
        ev = [
            b"\x09" + _pack_q(e.timestamp),
            _field(b"\x12", e.name.encode()),
            _attributes(b"\x1a", e.attributes),
        ]
        if e.dropped_attributes:
            ev.append(b"\x20" + _varint(e.dropped_attributes))
        parts.append(_field(b"\x5a", b"".join(ev)))
    if span.dropped_events:
        parts.append(b"\x60" + _varint(span.dropped_events))
    for link in span.links:
        lk = [
            b"\x0a\x10" + link.context.trace_id.to_bytes(16, "big"),
            b"\x12\x08" + link.context.span_id.to_bytes(8, "big"),
            _attributes(b"\x22", link.attributes),
        ]
        if link.dropped_attributes:
            lk.append(b"\x28" + _varint(link.dropped_attributes))
        lk.append(
            b"\x35"
            + _pack_i(_HAS_IS_REMOTE | (_IS_REMOTE if link.context.is_remote else 0))
        )
        parts.append(_field(b"\x6a", b"".join(lk)))
    if span.dropped_links:
        parts.append(b"\x70" + _varint(span.dropped_links))
    status = span.status
    st = []
    if status.description:
        st.append(_field(b"\x12", status.description.encode()))
    if status.status_code.value:
        st.append(b"\x18" + _varint(status.status_code.value))
    parts.append(_field(b"\x7a", b"".join(st)))
    parts.append(_flags(span.parent))
    return b"".join(parts)


def encode_spans(batch) -> bytes:
    """An ExportTraceServiceRequest's bytes for a batch of ReadableSpans."""
    return _request(
        batch, lambda s: s.resource, lambda s: s.instrumentation_scope or None, _span
    )


def log_tuple(rec) -> bytes:
    """A logs.v1.LogRecord's bytes from the pipeline's tuple: (time, severity
    number, severity text, body, attributes, trace id, span id, trace flags)."""
    ts, sev, sev_text, body, attrs, trace_id, span_id, flags = rec
    stamp = _pack_q(ts)
    parts = [
        b"\x09" + stamp,
        b"\x10" + _varint(sev),
        _any_value_field(b"\x1a", sev_text),
    ]
    if body is not None:
        parts.append(_field(b"\x2a", _any_value(body)))
    if attrs:
        parts.append(_attributes(b"\x32", attrs))
    if flags:
        parts.append(b"\x45" + _pack_i(flags))
    if trace_id:
        parts.append(b"\x4a\x10" + trace_id.to_bytes(16, "big"))
        parts.append(b"\x52\x08" + span_id.to_bytes(8, "big"))
    parts.append(b"\x59" + stamp)
    return b"".join(parts)


# Severity texts' field bytes: a handful, reused for every record.
_TEXTS: dict[str, bytes] = {}


def _any_value_field(tag: bytes, text: str) -> bytes:
    b = _TEXTS.get(text)
    if b is None:
        b = _TEXTS[text] = _field(tag, text.encode())
    return b


def encode_log_tuples(resource, scope, records) -> bytes:
    """An ExportLogsServiceRequest's bytes for the pipeline's records, all from
    one resource and scope."""
    scope_logs = [
        _field(b"\x0a", _cached(_SCOPES, scope, _encode_instrumentation_scope))
    ]
    scope_logs.extend(_field(b"\x12", log_tuple(r)) for r in records)
    resource_logs = [
        _field(b"\x0a", _cached(_RESOURCES, resource, _encode_resource)),
        _field(b"\x12", b"".join(scope_logs)),
    ]
    if resource.schema_url:
        resource_logs.append(_field(b"\x1a", resource.schema_url.encode()))
    return _field(b"\x0a", b"".join(resource_logs))
