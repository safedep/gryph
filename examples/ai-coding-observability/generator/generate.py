#!/usr/bin/env python3
"""
Synthetic data generator for Gryph audit events.

Usage:
    # Generate 7 days of backfill data and load into OpenSearch
    python generate.py --mode backfill --days 7 --load

    # Generate 7 days of backfill data as JSONL to stdout
    python generate.py --mode backfill --days 7

    # Stream events in real-time at 5 events/second
    python generate.py --mode stream --rate 5 --load
"""

import argparse
import json
import random
import re
import sys
import time
import uuid
from datetime import datetime, timedelta, timezone

import requests

from config import (
    OPENSEARCH_URL,
    INDEX_PREFIX,
    ENDPOINTS,
    AGENTS,
    SESSIONS_PER_DAY_MEAN,
    SESSIONS_PER_DAY_STD,
    EVENTS_PER_SESSION_MEAN,
    EVENTS_PER_SESSION_STD,
    WORK_HOURS_START,
    WORK_HOURS_END,
    OFF_HOURS_PROBABILITY,
    ACTION_WEIGHTS,
)
from templates import (
    COMMAND_EXEC_TEMPLATES,
    FILE_READ_TEMPLATES,
    FILE_WRITE_TEMPLATES,
    TOOL_USE_TEMPLATES,
    NOTIFICATION_TEMPLATES,
    WORKING_DIRECTORIES,
)
from scenarios import SCENARIOS

# Will be overridden by CLI args
_opensearch_url = OPENSEARCH_URL


def generate_event(
    endpoint, agent, session_id, sequence, timestamp, action_type, template_override=None
):
    """Generate a single Gryph audit event."""
    if template_override:
        tool_name = template_override.get(
            "tool_name", template_override.get("tool", "Bash")
        )
        payload = {k: v for k, v in template_override.get("payload", {}).items()}
        result_status = template_override.get("result_status", "success")
    else:
        tool_name, payload, result_status = _pick_template(action_type)

    for key in ("path", "command"):
        if key in payload and "{user}" in str(payload[key]):
            payload[key] = payload[key].replace("{user}", endpoint["username"])

    working_dir = random.choice(WORKING_DIRECTORIES).replace(
        "{user}", endpoint["username"]
    )

    duration_ms = random.randint(50, 30000)

    event = {
        "$schema": "https://raw.githubusercontent.com/safedep/gryph/main/schema/event.schema.json",
        "id": str(uuid.uuid4()),
        "session_id": session_id,
        "agent_session_id": session_id,
        "sequence": sequence,
        "timestamp": timestamp.strftime("%Y-%m-%dT%H:%M:%S.%fZ"),
        "agent_name": agent["name"],
        "agent_version": random.choice(agent.get("versions", ["0.0.0"])),
        "working_directory": working_dir,
        "action_type": action_type,
        "tool_name": tool_name,
        "result_status": result_status,
        "duration_ms": duration_ms,
        "payload": payload,
        "raw_event": {
            "hook_event_name": random.choice(["PreToolUse", "PostToolUse"]),
            "permission_mode": "default",
            "session_id": session_id,
            "tool_name": tool_name,
        },
        "is_sensitive": False,
        "endpoint_hostname": endpoint["hostname"],
        "endpoint_username": endpoint["username"],
    }

    if result_status == "error":
        event["error_message"] = random.choice([
            "Process exited with non-zero status",
            "Command timed out after 30s",
            "Permission denied",
            "File not found",
            "Connection refused",
            "Module not found",
            "Compilation failed",
            "Test assertion failed",
        ])

    return event


def _pick_template(action_type):
    """Select a random template for the given action type."""
    if action_type == "command_exec":
        t = random.choice(COMMAND_EXEC_TEMPLATES)
        result_status = "error" if t.get("exit_code", 0) != 0 else "success"
        return (
            "Bash",
            {
                "command": t["command"],
                "description": t["description"],
                "exit_code": t["exit_code"],
            },
            result_status,
        )
    elif action_type == "file_read":
        t = random.choice(FILE_READ_TEMPLATES)
        return random.choice(["Read", "Glob", "Grep"]), {"path": t["path"]}, "success"
    elif action_type == "file_write":
        t = random.choice(FILE_WRITE_TEMPLATES)
        return (
            random.choice(["Write", "Edit"]),
            {
                "path": t["path"],
                "lines_added": t["lines_added"],
                "lines_removed": t["lines_removed"],
            },
            "success",
        )
    elif action_type == "tool_use":
        t = random.choice(TOOL_USE_TEMPLATES)
        return (
            t["tool"],
            {"command": t["command"], "description": t["description"]},
            "success",
        )
    elif action_type == "notification":
        t = random.choice(NOTIFICATION_TEMPLATES)
        return (
            t["tool"],
            {"command": t["command"], "description": t["description"]},
            "success",
        )
    return "Unknown", {}, "success"


def generate_session(endpoint, agent, start_time, event_count):
    """Generate a complete session (session_start + events + session_end)."""
    session_id = str(uuid.uuid4())
    events = []
    current_time = start_time

    events.append(
        generate_event(
            endpoint,
            agent,
            session_id,
            0,
            current_time,
            "session_start",
            template_override={
                "tool_name": "",
                "payload": {},
                "result_status": "success",
            },
        )
    )

    action_types = list(ACTION_WEIGHTS.keys())
    action_probs = list(ACTION_WEIGHTS.values())

    for seq in range(1, event_count + 1):
        current_time += timedelta(seconds=random.uniform(5, 120))
        action_type = random.choices(action_types, weights=action_probs, k=1)[0]
        events.append(
            generate_event(endpoint, agent, session_id, seq, current_time, action_type)
        )

    current_time += timedelta(seconds=random.uniform(1, 10))
    events.append(
        generate_event(
            endpoint,
            agent,
            session_id,
            event_count + 1,
            current_time,
            "session_end",
            template_override={
                "tool_name": "",
                "payload": {},
                "result_status": "success",
            },
        )
    )

    return events


def inject_threat_scenarios(all_events, start_date, end_date):
    """Inject threat scenario events into the generated data."""
    for scenario in SCENARIOS:
        if scenario["name"] == "silent_endpoint":
            _apply_silent_endpoint(all_events, scenario, start_date)
            continue

        if scenario["name"] == "high_error_rate_endpoint":
            _apply_high_error_rate(all_events, scenario, start_date, end_date)
            continue

        events_to_inject = scenario.get("events", [])
        if not events_to_inject:
            continue

        freq = scenario.get("frequency", "1")
        count = _parse_frequency(freq)

        for _ in range(count):
            endpoint = random.choice(ENDPOINTS)
            agent = random.choices(
                AGENTS, weights=[a["weight"] for a in AGENTS], k=1
            )[0]
            session_id = str(uuid.uuid4())

            inject_hour = scenario.get("inject_at_hour")
            if inject_hour is not None:
                day_offset = random.randint(0, (end_date - start_date).days - 1)
                ts = (start_date + timedelta(days=day_offset)).replace(
                    hour=inject_hour,
                    minute=random.randint(0, 59),
                    second=random.randint(0, 59),
                    microsecond=random.randint(0, 999999),
                )
                burst_count = scenario.get("event_count", len(events_to_inject))
                for i in range(burst_count):
                    evt_template = random.choice(events_to_inject)
                    evt = generate_event(
                        endpoint,
                        agent,
                        session_id,
                        i + 1,
                        ts + timedelta(seconds=i * random.uniform(3, 15)),
                        evt_template["action_type"],
                        template_override=evt_template,
                    )
                    all_events.append(evt)
            else:
                ts = start_date + timedelta(
                    seconds=random.uniform(
                        0, (end_date - start_date).total_seconds()
                    )
                )
                for i, event_template in enumerate(events_to_inject):
                    evt = generate_event(
                        endpoint,
                        agent,
                        session_id,
                        i + 1,
                        ts + timedelta(seconds=i * random.uniform(5, 30)),
                        event_template["action_type"],
                        template_override=event_template,
                    )
                    all_events.append(evt)

    return all_events


def _apply_silent_endpoint(all_events, scenario, start_date):
    """Remove events from a target endpoint during a silent window."""
    target = scenario["target_endpoint"]
    offset_hours = scenario.get("silent_start_hour_offset", 72)
    duration_hours = scenario.get("silent_duration_hours", 8)

    silent_start = start_date + timedelta(hours=offset_hours)
    silent_end = silent_start + timedelta(hours=duration_hours)

    all_events[:] = [
        e
        for e in all_events
        if not (
            e["endpoint_hostname"] == target
            and silent_start
            <= datetime.strptime(e["timestamp"], "%Y-%m-%dT%H:%M:%S.%fZ").replace(
                tzinfo=timezone.utc
            )
            <= silent_end
        )
    ]


def _apply_high_error_rate(all_events, scenario, start_date, end_date):
    """Increase error rate for a target endpoint during a window."""
    target = scenario["target_endpoint"]
    error_rate = scenario.get("error_rate", 0.35)

    window_start = start_date + timedelta(
        seconds=random.uniform(0, (end_date - start_date).total_seconds() * 0.8)
    )
    window_end = window_start + timedelta(hours=scenario.get("duration_hours", 2))

    for event in all_events:
        if event["endpoint_hostname"] != target:
            continue
        evt_ts = datetime.strptime(
            event["timestamp"], "%Y-%m-%dT%H:%M:%S.%fZ"
        ).replace(tzinfo=timezone.utc)
        if window_start <= evt_ts <= window_end:
            if random.random() < error_rate:
                event["result_status"] = "error"
                if "exit_code" in event.get("payload", {}):
                    event["payload"]["exit_code"] = 1


def _parse_frequency(freq_str):
    """Parse frequency string like '2-3' into a random count."""
    match = re.match(r"(\d+)(?:-(\d+))?", freq_str)
    if match:
        low = int(match.group(1))
        high = int(match.group(2)) if match.group(2) else low
        return random.randint(low, high)
    return 1


def bulk_load(events, opensearch_url, index_prefix):
    """Load events into OpenSearch via _bulk API."""
    by_index = {}
    for event in events:
        ts = datetime.strptime(event["timestamp"], "%Y-%m-%dT%H:%M:%S.%fZ")
        index_name = f"{index_prefix}-{ts.strftime('%Y.%m')}"
        by_index.setdefault(index_name, []).append(event)

    total = 0
    for index_name, index_events in sorted(by_index.items()):
        for chunk_start in range(0, len(index_events), 5000):
            chunk = index_events[chunk_start : chunk_start + 5000]
            lines = []
            for event in chunk:
                action = {"index": {"_index": index_name, "_id": event["id"]}}
                lines.append(json.dumps(action))
                lines.append(json.dumps(event))
            body = "\n".join(lines) + "\n"

            resp = requests.post(
                f"{opensearch_url}/_bulk",
                headers={"Content-Type": "application/x-ndjson"},
                data=body,
                timeout=60,
            )
            resp.raise_for_status()
            result = resp.json()
            if result.get("errors"):
                error_items = [
                    item
                    for item in result["items"]
                    if "error" in item.get("index", {})
                ]
                print(
                    f"  WARNING: {len(error_items)} errors in bulk load to {index_name}",
                    file=sys.stderr,
                )
            total += len(chunk)

        print(
            f"  Loaded {len(index_events):,} events into {index_name}",
            file=sys.stderr,
        )

    return total


def generate_backfill(days, load):
    """Generate historical data."""
    end_date = datetime.now(timezone.utc)
    start_date = end_date - timedelta(days=days)
    all_events = []

    print(
        f"Generating {days} days of data for {len(ENDPOINTS)} endpoints...",
        file=sys.stderr,
    )

    for endpoint in ENDPOINTS:
        endpoint_events = 0
        current_date = start_date.replace(hour=0, minute=0, second=0, microsecond=0)
        while current_date < end_date:
            n_sessions = max(
                1, int(random.gauss(SESSIONS_PER_DAY_MEAN, SESSIONS_PER_DAY_STD))
            )

            for _ in range(n_sessions):
                agent = random.choices(
                    AGENTS, weights=[a["weight"] for a in AGENTS], k=1
                )[0]
                n_events = max(
                    5,
                    int(
                        random.gauss(EVENTS_PER_SESSION_MEAN, EVENTS_PER_SESSION_STD)
                    ),
                )

                if random.random() < OFF_HOURS_PROBABILITY:
                    hour = random.choice(
                        [*range(0, WORK_HOURS_START), *range(WORK_HOURS_END, 24)]
                    )
                else:
                    hour = random.randint(WORK_HOURS_START, WORK_HOURS_END - 1)

                session_start = current_date.replace(
                    hour=hour,
                    minute=random.randint(0, 59),
                    second=random.randint(0, 59),
                    microsecond=random.randint(0, 999999),
                )

                events = generate_session(endpoint, agent, session_start, n_events)
                all_events.extend(events)
                endpoint_events += len(events)

            current_date += timedelta(days=1)

        print(
            f"  {endpoint['hostname']}: {endpoint_events:,} events", file=sys.stderr
        )

    print("Injecting threat scenarios...", file=sys.stderr)
    inject_threat_scenarios(all_events, start_date, end_date)

    all_events.sort(key=lambda e: e["timestamp"])

    print(f"Total: {len(all_events):,} events", file=sys.stderr)

    if load:
        print(f"Loading into OpenSearch at {_opensearch_url}...", file=sys.stderr)
        total = bulk_load(all_events, _opensearch_url, INDEX_PREFIX)
        print(f"Done. Loaded {total:,} events.", file=sys.stderr)
    else:
        for event in all_events:
            print(json.dumps(event))


def generate_stream(rate, load):
    """Stream events in real-time."""
    print(
        f"Streaming events at ~{rate}/sec. Press Ctrl+C to stop.", file=sys.stderr
    )
    buffer = []
    last_flush = time.time()
    total_streamed = 0

    try:
        while True:
            endpoint = random.choice(ENDPOINTS)
            agent = random.choices(
                AGENTS, weights=[a["weight"] for a in AGENTS], k=1
            )[0]
            session_id = str(uuid.uuid4())

            n_events = random.randint(1, 3)
            action_types = list(ACTION_WEIGHTS.keys())
            action_probs = list(ACTION_WEIGHTS.values())

            for seq in range(n_events):
                action_type = random.choices(
                    action_types, weights=action_probs, k=1
                )[0]
                event = generate_event(
                    endpoint,
                    agent,
                    session_id,
                    seq,
                    datetime.now(timezone.utc),
                    action_type,
                )

                if load:
                    buffer.append(event)
                else:
                    print(json.dumps(event))
                    sys.stdout.flush()

                total_streamed += 1

            if load and (time.time() - last_flush) >= 5:
                if buffer:
                    n = len(buffer)
                    bulk_load(buffer, _opensearch_url, INDEX_PREFIX)
                    buffer = []
                    print(
                        f"  Streamed {total_streamed:,} events total",
                        file=sys.stderr,
                    )
                last_flush = time.time()

            time.sleep(1.0 / rate)

    except KeyboardInterrupt:
        if load and buffer:
            bulk_load(buffer, _opensearch_url, INDEX_PREFIX)
        print(f"\nStream stopped. Total: {total_streamed:,} events.", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description="Gryph synthetic data generator")
    parser.add_argument(
        "--mode", choices=["backfill", "stream"], required=True, help="Generation mode"
    )
    parser.add_argument(
        "--days", type=int, default=7, help="Days of backfill data (default: 7)"
    )
    parser.add_argument(
        "--rate", type=float, default=5.0, help="Events per second for stream mode (default: 5)"
    )
    parser.add_argument(
        "--load", action="store_true", help="Load directly into OpenSearch"
    )
    parser.add_argument(
        "--opensearch-url",
        default=None,
        help=f"OpenSearch URL (default: {OPENSEARCH_URL})",
    )
    args = parser.parse_args()

    global _opensearch_url
    _opensearch_url = args.opensearch_url or OPENSEARCH_URL

    if args.mode == "backfill":
        generate_backfill(args.days, args.load)
    elif args.mode == "stream":
        generate_stream(args.rate, args.load)


if __name__ == "__main__":
    main()
