#!/usr/bin/env python3
"""Emit deterministic, synthetic protocol-v1 records for the demo database."""

import argparse
import json
import sys


EVENTS = [
    {
        "id": "ledger-codex-001",
        "at": "2026-10-18T09:00:00Z",
        "project": "project-ledger",
        "project_name": "Ledger Studio",
        "root": "/synthetic/workspaces/ledger-studio",
        "session": "session-ledger-codex",
        "client": "codex-cli",
        "provider": "openai",
        "model": "gpt-5-codex",
        "usage": {
            "input_tokens": 80000,
            "cache_read_tokens": 30000,
            "cache_write_tokens": 0,
            "output_tokens": 8000,
            "reasoning_tokens": 2000,
        },
    },
    {
        "id": "ledger-codex-002",
        "at": "2026-10-18T09:07:00Z",
        "project": "project-ledger",
        "project_name": "Ledger Studio",
        "root": "/synthetic/workspaces/ledger-studio",
        "session": "session-ledger-codex",
        "client": "codex-cli",
        "provider": "openai",
        "model": "gpt-5-codex",
        "usage": {
            "input_tokens": 120000,
            "cache_read_tokens": 80000,
            "cache_write_tokens": 0,
            "output_tokens": 14000,
            "reasoning_tokens": 5000,
        },
    },
    {
        "id": "ledger-claude-001",
        "at": "2026-10-18T10:15:00Z",
        "project": "project-ledger",
        "project_name": "Ledger Studio",
        "root": "/synthetic/workspaces/ledger-studio",
        "session": "session-ledger-claude",
        "client": "claude-code",
        "provider": "anthropic",
        "model": "claude-sonnet-5-5",
        "usage": {
            "input_tokens": 50000,
            "cache_read_tokens": 22000,
            "cache_write_tokens": 0,
            "output_tokens": 2400,
            "reasoning_tokens": 0,
        },
    },
    {
        "id": "orbit-claude-001",
        "at": "2026-10-19T13:20:00Z",
        "project": "project-orbit",
        "project_name": "Orbit Notes",
        "root": "/synthetic/workspaces/orbit-notes",
        "session": "session-orbit-claude",
        "client": "claude-code",
        "provider": "anthropic",
        "model": "claude-sonnet-5-5",
        "usage": {
            "input_tokens": 60000,
            "cache_read_tokens": 20000,
            "cache_write_tokens": 0,
            "output_tokens": 5000,
            "reasoning_tokens": 0,
        },
    },
    {
        "id": "orbit-claude-002",
        "at": "2026-10-19T13:28:00Z",
        "project": "project-orbit",
        "project_name": "Orbit Notes",
        "root": "/synthetic/workspaces/orbit-notes",
        "session": "session-orbit-claude",
        "client": "claude-code",
        "provider": "anthropic",
        "model": "claude-sonnet-5-5",
        "usage": {
            "input_tokens": 25000,
            "cache_read_tokens": 10000,
            "cache_write_tokens": 0,
            "output_tokens": 1500,
            "reasoning_tokens": 0,
        },
    },
]


def emit(value):
    print(json.dumps(value, separators=(",", ":")))


def event_record(item):
    event_id = item["id"]
    event = {
        "event_id": event_id,
        "schema_version": 1,
        "event_type": "model.request",
        "occurred_at": item["at"],
        "client": {"name": item["client"]},
        "provider": item["provider"],
        "requested_model": item["model"],
        "resolved_model": item["model"],
        "usage": item["usage"],
        "project_id": item["project"],
        "session_id": item["session"],
        "provenance": {
            "adapter": "synthetic-demo",
            "source_id": "synthetic-demo-fixture",
            "source_record": event_id,
        },
        "ingestion_key": "synthetic-demo-fixture:" + event_id,
    }
    # Deliberately omit Claude billing_context. The demo must show that the
    # public estimate remains unknown without the required observed evidence.
    return {
        "protocol": "komposyn.token-logger",
        "version": 1,
        "id": event_id,
        "method": "record",
        "params": {
            "event": event,
            "project": {
                "id": item["project"],
                "root": item["root"],
                "display_name": item["project_name"],
            },
            "session": {
                "id": item["session"],
                "project_id": item["project"],
                "client": {"name": item["client"]},
            },
        },
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("data_dir", help="absolute, isolated application data directory")
    args = parser.parse_args()
    if not args.data_dir.startswith("/"):
        parser.error("data_dir must be an absolute path")

    emit({
        "protocol": "komposyn.token-logger",
        "version": 1,
        "id": "initialize",
        "method": "initialize",
        "params": {"data_dir": args.data_dir},
    })
    for item in EVENTS:
        emit(event_record(item))
    emit({
        "protocol": "komposyn.token-logger",
        "version": 1,
        "id": "shutdown",
        "method": "shutdown",
    })


if __name__ == "__main__":
    main()
