#!/usr/bin/env python3
"""Independent frozen-vector checker for BRC-136 BASM and TAC values.

This script reads vectors.json and only verifies it; it never rewrites expected
values. It deliberately uses Python's standard-library hashlib implementation.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path


HEX32 = 64


def display_to_internal(value: str) -> bytes:
    if len(value) != HEX32 or any(c not in "0123456789abcdef" for c in value):
        raise ValueError(f"expected lowercase 32-byte hex, got {value!r}")
    return bytes.fromhex(value)[::-1]


def internal_to_display(value: bytes) -> str:
    if len(value) != 32:
        raise ValueError("expected 32 bytes")
    return value[::-1].hex()


def sha256d(value: bytes) -> bytes:
    import hashlib

    return hashlib.sha256(hashlib.sha256(value).digest()).digest()


def sha256d_openssl(value: bytes) -> bytes:
    """Independent double-SHA256 through the OpenSSL command line."""
    first = subprocess.run(
        ["openssl", "dgst", "-sha256", "-binary"],
        input=value,
        capture_output=True,
        check=True,
    ).stdout
    return subprocess.run(
        ["openssl", "dgst", "-sha256", "-binary"],
        input=first,
        capture_output=True,
        check=True,
    ).stdout


def basm_root(txids: list[str]) -> str:
    if not txids:
        return "00" * 32
    layer = [display_to_internal(txid) for txid in txids]
    while len(layer) > 1:
        layer = [
            sha256d(layer[index] + layer[index + 1 if index + 1 < len(layer) else index])
            for index in range(0, len(layer), 2)
        ]
    return internal_to_display(layer[0])


def tac_value(previous: str, block_hash: str, root: str) -> str:
    return internal_to_display(
        sha256d(
            display_to_internal(previous)
            + display_to_internal(block_hash)
            + display_to_internal(root)
        )
    )


def check(data: dict) -> int:
    zero = "00" * 32
    if data.get("hashEncoding") != "lowercase display-order hex":
        raise AssertionError("unexpected hash encoding declaration")

    for fixture in data["byteOrder"]:
        display = fixture["display"]
        expected_internal = fixture["internal"]
        actual_internal = display_to_internal(display).hex()
        if actual_internal != expected_internal:
            raise AssertionError(f"byte-order fixture {fixture['name']} failed")
        if internal_to_display(bytes.fromhex(expected_internal)) != display:
            raise AssertionError(f"byte-order round trip {fixture['name']} failed")

    openssl_checks = 0
    merkle_by_name = {}
    for fixture in data["merkle"]:
        actual = basm_root(fixture["txids"])
        if actual != fixture["root"]:
            raise AssertionError(
                f"BASM {fixture['name']}: {actual} != {fixture['root']}"
            )
        if len(fixture["txids"]) >= 2:
            layer = [display_to_internal(txid) for txid in fixture["txids"]]
            while len(layer) > 1:
                layer = [
                    sha256d_openssl(
                        layer[index]
                        + layer[index + 1 if index + 1 < len(layer) else index]
                    )
                    for index in range(0, len(layer), 2)
                ]
            openssl_root = internal_to_display(layer[0])
            if openssl_root != actual:
                raise AssertionError(
                    f"OpenSSL BASM {fixture['name']}: {openssl_root} != {actual}"
                )
            openssl_checks += 1
        if fixture["admissionListValid"]:
            if len(set(fixture["txids"])) != len(fixture["txids"]):
                raise AssertionError(f"valid fixture {fixture['name']} has duplicates")
        merkle_by_name[fixture["name"]] = fixture

    for fixture in data["tac"]:
        previous = zero
        expected_height = fixture["genesisHeight"]
        for anchor in fixture["anchors"]:
            if anchor["blockHeight"] != expected_height:
                raise AssertionError(f"non-contiguous heights in {fixture['name']}")
            if not isinstance(anchor["admittedCount"], int) or anchor["admittedCount"] < 0:
                raise AssertionError(f"invalid admitted count in {fixture['name']}")
            if not isinstance(anchor["rootSource"], str) or not anchor["rootSource"]:
                raise AssertionError(f"missing root source in {fixture['name']}")
            source = merkle_by_name.get(anchor["rootSource"])
            if source is None:
                raise AssertionError(f"unknown root source {anchor['rootSource']!r}")
            if source["root"] != anchor["basmRoot"]:
                raise AssertionError(f"root source mismatch in {fixture['name']}")
            if len(source["txids"]) != anchor["admittedCount"]:
                raise AssertionError(f"admitted count mismatch in {fixture['name']}")
            actual = tac_value(previous, anchor["blockHash"], anchor["basmRoot"])
            if actual != anchor["expectedTac"]:
                raise AssertionError(
                    f"TAC {fixture['name']} height {anchor['blockHeight']}: "
                    f"{actual} != {anchor['expectedTac']}"
                )
            openssl_actual = internal_to_display(
                sha256d_openssl(
                    display_to_internal(previous)
                    + display_to_internal(anchor["blockHash"])
                    + display_to_internal(anchor["basmRoot"])
                )
            )
            if openssl_actual != actual:
                raise AssertionError(
                    f"OpenSSL TAC {fixture['name']} height "
                    f"{anchor['blockHeight']}: {openssl_actual} != {actual}"
                )
            openssl_checks += 1
            previous = actual
            expected_height += 1
    return openssl_checks


def main() -> int:
    path = Path(__file__).with_name("vectors.json")
    try:
        openssl_checks = check(json.loads(path.read_text(encoding="utf-8")))
    except (OSError, ValueError, KeyError, AssertionError, subprocess.SubprocessError) as error:
        print(f"verify_vectors.py: FAIL: {error}", file=sys.stderr)
        return 1
    print(f"verify_vectors.py: OK ({path}; OpenSSL cross-checks: {openssl_checks})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
