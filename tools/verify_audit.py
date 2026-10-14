#!/usr/bin/env python3
"""Independently verify a LabRat audit chain export. Standard library only.

    curl -s localhost:8080/v1/audit/export.ndjson \
         -H 'X-LDE-Tenant: ...' -H 'X-LDE-Actor: auditor' > chain.ndjson
    python3 tools/verify_audit.py chain.ndjson

This file deliberately shares no code with the LabRat server. If it agrees
with the server, the chain is intact; if they ever disagree, trust this one.
Spec: docs/audit-chain.md
"""
import hashlib
import json
import struct
import sys
from datetime import datetime, timezone

GENESIS = bytes(32)
EPOCH = datetime(1970, 1, 1, tzinfo=timezone.utc)


def unix_micros(rfc3339: str) -> int:
    # Go emits up to 9 fractional digits; Python parses at most 6. LabRat
    # timestamps are microsecond-truncated, so the extra digits are zeros.
    head, frac, tz = rfc3339, "", ""
    if rfc3339.endswith("Z"):
        head, tz = rfc3339[:-1], "+00:00"
    else:
        head, tz = rfc3339[:-6], rfc3339[-6:]
    if "." in head:
        head, frac = head.split(".")
    dt = datetime.fromisoformat(head + tz)
    delta = dt - EPOCH
    return (delta.days * 86400 + delta.seconds) * 1_000_000 + int((frac + "000000")[:6])


def lp(s: str) -> bytes:
    b = s.encode("utf-8")
    return struct.pack(">Q", len(b)) + b


def digest(r: dict) -> bytes:
    m = b"LDE-AUDIT-v1"
    m += struct.pack(">q", r["seq"])
    m += struct.pack(">q", unix_micros(r["occurred_at"]))
    for f in ("tenant_id", "actor", "action", "entity_type", "entity_id", "reason", "payload"):
        m += lp(r.get(f, ""))
    m += bytes.fromhex(r["prev_hash"])
    return hashlib.sha256(m).digest()


def main(path: str) -> int:
    prev_seq, prev_hash, n = 0, GENESIS, 0
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            if not line.strip():
                continue
            r = json.loads(line)
            seq = r["seq"]
            if seq != prev_seq + 1:
                print(f"BROKEN at seq {seq}: expected seq {prev_seq + 1}")
                return 1
            if bytes.fromhex(r["prev_hash"]) != prev_hash:
                print(f"BROKEN at seq {seq}: prev_hash does not match preceding event")
                return 1
            if digest(r) != bytes.fromhex(r["hash"]):
                print(f"BROKEN at seq {seq}: content does not match hash (record altered)")
                return 1
            prev_seq, prev_hash, n = seq, bytes.fromhex(r["hash"]), n + 1
    print(f"OK: {n} events, head seq {prev_seq}, head hash {prev_hash.hex()}")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    sys.exit(main(sys.argv[1]))
