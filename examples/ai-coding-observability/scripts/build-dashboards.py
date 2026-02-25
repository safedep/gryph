#!/usr/bin/env python3
"""
Build OpenSearch Dashboards saved objects (NDJSON) for all four dashboards.

Usage:
    python scripts/build-dashboards.py

Outputs NDJSON files into dashboards/ directory.
"""

import json
import os
import uuid

DASHBOARDS_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "dashboards")
INDEX_PATTERN_ID = "gryph-events-*"


def vis_id():
    return str(uuid.uuid4())[:8]


def write_ndjson(filename, objects):
    path = os.path.join(DASHBOARDS_DIR, filename)
    with open(path, "w") as f:
        for obj in objects:
            f.write(json.dumps(obj, separators=(",", ":")) + "\n")
    print(f"  Wrote {path} ({len(objects)} objects)")


def make_vis_state_metric(title, agg_type="count", field=None, custom_label=None):
    aggs = [{"id": "1", "enabled": True, "type": agg_type, "schema": "metric", "params": {}}]
    if field:
        aggs[0]["params"]["field"] = field
    if custom_label:
        aggs[0]["params"]["customLabel"] = custom_label
    return json.dumps({
        "title": title,
        "type": "metric",
        "aggs": aggs,
        "params": {
            "addTooltip": True,
            "addLegend": False,
            "type": "metric",
            "metric": {
                "percentageMode": False,
                "useRanges": False,
                "colorSchema": "Green to Red",
                "metricColorMode": "None",
                "colorsRange": [{"from": 0, "to": 10000}],
                "labels": {"show": True},
                "invertColors": False,
                "style": {"bgFill": "#000", "bgColor": False, "labelColor": False, "subText": "", "fontSize": 60},
            },
        },
    })


def make_vis_state_area(title):
    return json.dumps({
        "title": title,
        "type": "area",
        "aggs": [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
            {"id": "2", "enabled": True, "type": "date_histogram", "schema": "segment", "params": {
                "field": "timestamp", "interval": "auto", "min_doc_count": 1, "extended_bounds": {},
            }},
            {"id": "3", "enabled": True, "type": "terms", "schema": "group", "params": {
                "field": "action_type", "size": 10, "order": "desc", "orderBy": "1",
            }},
        ],
        "params": {
            "type": "area", "grid": {"categoryLines": False}, "categoryAxes": [{"id": "CategoryAxis-1", "type": "category", "position": "bottom", "show": True, "labels": {"show": True, "filter": True, "truncate": 100}}],
            "valueAxes": [{"id": "ValueAxis-1", "name": "LeftAxis-1", "type": "value", "position": "left", "show": True, "labels": {"show": True, "rotate": 0, "filter": False, "truncate": 100}}],
            "addTooltip": True, "addLegend": True, "legendPosition": "right",
            "seriesParams": [{"show": True, "type": "area", "mode": "stacked", "data": {"label": "Count", "id": "1"}, "valueAxis": "ValueAxis-1"}],
        },
    })


def make_vis_state_pie(title, field, size=10):
    return json.dumps({
        "title": title,
        "type": "pie",
        "aggs": [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "segment", "params": {
                "field": field, "size": size, "order": "desc", "orderBy": "1",
            }},
        ],
        "params": {"type": "pie", "addTooltip": True, "addLegend": True, "legendPosition": "right", "isDonut": True, "labels": {"show": True, "values": True, "last_level": True, "truncate": 100}},
    })


def make_vis_state_hbar(title, field, size=10):
    return json.dumps({
        "title": title,
        "type": "horizontal_bar",
        "aggs": [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "segment", "params": {
                "field": field, "size": size, "order": "desc", "orderBy": "1",
            }},
        ],
        "params": {
            "type": "horizontal_bar", "addTooltip": True, "addLegend": True, "legendPosition": "right",
            "categoryAxes": [{"id": "CategoryAxis-1", "type": "category", "position": "left", "show": True, "labels": {"show": True, "filter": False, "truncate": 200}}],
            "valueAxes": [{"id": "ValueAxis-1", "name": "BottomAxis-1", "type": "value", "position": "bottom", "show": True, "labels": {"show": True, "rotate": 0, "filter": False, "truncate": 100}}],
        },
    })


def make_vis_state_table(title, aggs):
    return json.dumps({
        "title": title,
        "type": "table",
        "aggs": aggs,
        "params": {"perPage": 20, "showPartialRows": False, "showMetricsAtAllLevels": False, "showTotal": False, "totalFunc": "sum", "percentageCol": ""},
    })


def make_vis_state_line(title, filter_field=None, filter_value=None):
    aggs = [
        {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
        {"id": "2", "enabled": True, "type": "date_histogram", "schema": "segment", "params": {
            "field": "timestamp", "interval": "auto", "min_doc_count": 1, "extended_bounds": {},
        }},
    ]
    return json.dumps({
        "title": title,
        "type": "line",
        "aggs": aggs,
        "params": {
            "type": "line", "grid": {"categoryLines": False},
            "categoryAxes": [{"id": "CategoryAxis-1", "type": "category", "position": "bottom", "show": True, "labels": {"show": True, "filter": True, "truncate": 100}}],
            "valueAxes": [{"id": "ValueAxis-1", "name": "LeftAxis-1", "type": "value", "position": "left", "show": True, "labels": {"show": True, "rotate": 0, "filter": False, "truncate": 100}}],
            "addTooltip": True, "addLegend": True, "legendPosition": "right",
        },
    })


def make_vis_state_markdown(title, markdown_text):
    return json.dumps({
        "title": title,
        "type": "markdown",
        "params": {"markdown": markdown_text, "fontSize": 12},
        "aggs": [],
    })


def make_vis_state_bar(title, field, size=10, split_field=None):
    aggs = [
        {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
        {"id": "2", "enabled": True, "type": "terms", "schema": "segment", "params": {
            "field": field, "size": size, "order": "desc", "orderBy": "1",
        }},
    ]
    if split_field:
        aggs.append({"id": "3", "enabled": True, "type": "terms", "schema": "group", "params": {
            "field": split_field, "size": 5, "order": "desc", "orderBy": "1",
        }})
    return json.dumps({
        "title": title,
        "type": "histogram",
        "aggs": aggs,
        "params": {
            "type": "histogram", "addTooltip": True, "addLegend": True, "legendPosition": "right",
            "categoryAxes": [{"id": "CategoryAxis-1", "type": "category", "position": "bottom", "show": True, "labels": {"show": True, "filter": True, "truncate": 100, "rotate": -45}}],
            "valueAxes": [{"id": "ValueAxis-1", "name": "LeftAxis-1", "type": "value", "position": "left", "show": True, "labels": {"show": True, "rotate": 0, "filter": False, "truncate": 100}}],
            "seriesParams": [{"show": True, "type": "histogram", "mode": "stacked", "data": {"label": "Count", "id": "1"}, "valueAxis": "ValueAxis-1"}],
        },
    })


def make_vis_state_heatmap(title):
    return json.dumps({
        "title": title,
        "type": "heatmap",
        "aggs": [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
            {"id": "2", "enabled": True, "type": "date_histogram", "schema": "segment", "params": {
                "field": "timestamp", "interval": "h", "min_doc_count": 0, "extended_bounds": {},
                "customLabel": "Hour of Day",
            }},
            {"id": "3", "enabled": True, "type": "terms", "schema": "group", "params": {
                "field": "endpoint_hostname", "size": 20, "order": "desc", "orderBy": "1",
                "customLabel": "Endpoint",
            }},
        ],
        "params": {
            "type": "heatmap", "addTooltip": True, "addLegend": True, "legendPosition": "right",
            "colorsNumber": 8, "colorSchema": "Greens", "invertColors": False,
            "percentageMode": False, "valueAxes": [{"show": False, "id": "ValueAxis-1", "type": "value", "labels": {"show": False, "rotate": 0, "overwriteColor": False, "color": "#555"}}],
        },
    })


def saved_visualization(vid, title, vis_state_json, index_pattern_id, search_source=None):
    """Create a saved visualization object."""
    kibanaSavedObjectMeta = {}
    if search_source:
        kibanaSavedObjectMeta["searchSourceJSON"] = json.dumps(search_source)
    else:
        kibanaSavedObjectMeta["searchSourceJSON"] = json.dumps({
            "index": index_pattern_id,
            "query": {"query": "", "language": "kuery"},
            "filter": [],
        })

    return {
        "id": vid,
        "type": "visualization",
        "attributes": {
            "title": title,
            "visState": vis_state_json,
            "uiStateJSON": "{}",
            "description": "",
            "kibanaSavedObjectMeta": kibanaSavedObjectMeta,
        },
        "references": [
            {"name": "kibanaSavedObjectMeta.searchSourceJSON.index", "type": "index-pattern", "id": index_pattern_id}
        ],
    }


def saved_search(sid, title, index_pattern_id, columns, query_filter=None):
    """Create a saved search object."""
    search_source = {
        "index": index_pattern_id,
        "query": {"query": query_filter or "", "language": "kuery"},
        "filter": [],
        "highlightAll": True,
        "version": True,
    }
    return {
        "id": sid,
        "type": "search",
        "attributes": {
            "title": title,
            "description": "",
            "columns": columns,
            "sort": [["timestamp", "desc"]],
            "kibanaSavedObjectMeta": {
                "searchSourceJSON": json.dumps(search_source),
            },
        },
        "references": [
            {"name": "kibanaSavedObjectMeta.searchSourceJSON.index", "type": "index-pattern", "id": index_pattern_id}
        ],
    }


def saved_dashboard(did, title, panels, time_from="now-24h", time_to="now"):
    """Create a saved dashboard object."""
    panels_json = []
    references = []
    for i, (panel_id, panel_type, title_str, grid) in enumerate(panels):
        ref_name = f"panel_{i}"
        panels_json.append({
            "version": "2.19.1",
            "gridData": {"x": grid[0], "y": grid[1], "w": grid[2], "h": grid[3], "i": str(i)},
            "panelIndex": str(i),
            "embeddableConfig": {"title": title_str},
            "panelRefName": ref_name,
        })
        references.append({
            "name": ref_name,
            "type": panel_type,
            "id": panel_id,
        })

    return {
        "id": did,
        "type": "dashboard",
        "attributes": {
            "title": title,
            "description": "",
            "panelsJSON": json.dumps(panels_json),
            "optionsJSON": json.dumps({"hidePanelTitles": False, "useMargins": True}),
            "timeRestore": True,
            "timeTo": time_to,
            "timeFrom": time_from,
            "kibanaSavedObjectMeta": {
                "searchSourceJSON": json.dumps({"query": {"query": "", "language": "kuery"}, "filter": []}),
            },
        },
        "references": references,
    }


# ============================================================
# SOC Overview Dashboard
# ============================================================
def build_soc_overview():
    objects = []
    panels = []
    idx = INDEX_PATTERN_ID

    vid = "soc-total-events"
    objects.append(saved_visualization(vid, "Total Events (24h)",
        make_vis_state_metric("Total Events (24h)", "count", custom_label="Events"), idx))
    panels.append((vid, "visualization", "Total Events (24h)", (0, 0, 12, 8)))

    vid = "soc-active-endpoints"
    objects.append(saved_visualization(vid, "Active Endpoints",
        make_vis_state_metric("Active Endpoints", "cardinality", "endpoint_hostname", "Endpoints"), idx))
    panels.append((vid, "visualization", "Active Endpoints", (12, 0, 12, 8)))

    vid = "soc-active-sessions"
    objects.append(saved_visualization(vid, "Active Sessions",
        make_vis_state_metric("Active Sessions", "cardinality", "session_id", "Sessions"), idx))
    panels.append((vid, "visualization", "Active Sessions", (24, 0, 12, 8)))

    vid = "soc-error-count"
    objects.append(saved_visualization(vid, "Errors",
        make_vis_state_metric("Errors", "count", custom_label="Errors"), idx,
        search_source={"index": idx, "query": {"query": "result_status:error", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "Errors", (36, 0, 12, 8)))

    vid = "soc-events-over-time"
    objects.append(saved_visualization(vid, "Events Over Time",
        make_vis_state_area("Events Over Time"), idx))
    panels.append((vid, "visualization", "Events Over Time", (0, 8, 48, 14)))

    vid = "soc-action-breakdown"
    objects.append(saved_visualization(vid, "Action Type Breakdown",
        make_vis_state_pie("Action Type Breakdown", "action_type"), idx))
    panels.append((vid, "visualization", "Action Type Breakdown", (0, 22, 20, 14)))

    vid = "soc-agent-distribution"
    objects.append(saved_visualization(vid, "Agent Distribution",
        make_vis_state_hbar("Agent Distribution", "agent_name"), idx))
    panels.append((vid, "visualization", "Agent Distribution", (20, 22, 28, 14)))

    vid = "soc-top-endpoints"
    objects.append(saved_visualization(vid, "Top 10 Active Endpoints",
        make_vis_state_table("Top 10 Active Endpoints", [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {"customLabel": "Events"}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "bucket", "params": {"field": "endpoint_hostname", "size": 10, "order": "desc", "orderBy": "1", "customLabel": "Endpoint"}},
        ]), idx))
    panels.append((vid, "visualization", "Top 10 Active Endpoints", (0, 36, 48, 12)))

    sid = "soc-recent-errors"
    objects.append(saved_search(sid, "Recent Errors",
        idx, ["timestamp", "endpoint_hostname", "agent_name", "tool_name", "payload.command"],
        "result_status:error"))
    panels.append((sid, "search", "Recent Errors", (0, 48, 48, 12)))

    vid = "soc-commands-over-time"
    objects.append(saved_visualization(vid, "Command Executions Over Time",
        make_vis_state_line("Command Executions Over Time"), idx,
        search_source={"index": idx, "query": {"query": "action_type:command_exec", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "Command Executions Over Time", (0, 60, 48, 12)))

    dashboard = saved_dashboard("soc-overview", "SOC Overview", panels)
    objects.append(dashboard)

    write_ndjson("soc-overview.ndjson", objects)


# ============================================================
# Threat Detection Dashboard
# ============================================================
def build_threat_detection():
    objects = []
    panels = []
    idx = INDEX_PATTERN_ID

    vid = "threat-summary"
    objects.append(saved_visualization(vid, "Threat Summary (24h)",
        make_vis_state_markdown("Threat Summary (24h)",
            "# Threat Detection Dashboard\n\n"
            "This dashboard highlights **potential security threats** from AI coding agent activity.\n\n"
            "| Threat Category | What to Look For |\n"
            "|---|---|\n"
            "| **Suspicious Commands** | curl POST, wget, netcat, base64 decode, eval |\n"
            "| **Credential Access** | .env, .pem, .key, .ssh, .aws reads |\n"
            "| **Supply Chain** | Package installs (npm, pip, yarn, cargo) |\n"
            "| **Data Exfiltration** | HTTP POST to external URLs |\n"
            "| **CI/CD Tampering** | Docker, workflow, Makefile modifications |\n"
            "| **MCP Tool Abuse** | WebFetch, WebSearch, Slack MCP tools |\n"),
        idx))
    panels.append((vid, "visualization", "Threat Summary (24h)", (0, 0, 48, 10)))

    sid = "threat-suspicious-commands"
    objects.append(saved_search(sid, "Suspicious Commands", idx,
        ["timestamp", "endpoint_hostname", "endpoint_username", "agent_name", "payload.command"],
        "action_type:command_exec AND (payload.command:curl AND payload.command:POST) OR payload.command:wget OR payload.command:\"nc \" OR payload.command:ncat OR payload.command:base64 OR payload.command:eval OR (payload.command:python3 AND payload.command:\"-c\")"))
    panels.append((sid, "search", "Suspicious Commands", (0, 10, 48, 12)))

    sid = "threat-sensitive-files"
    objects.append(saved_search(sid, "Sensitive File Access Attempts", idx,
        ["timestamp", "endpoint_hostname", "endpoint_username", "agent_name", "payload.path"],
        "action_type:file_read AND (payload.path:*.env* OR payload.path:*.pem OR payload.path:*.key OR payload.path:*credential* OR payload.path:*secret* OR payload.path:*.ssh* OR payload.path:*.aws*)"))
    panels.append((sid, "search", "Sensitive File Access Attempts", (0, 22, 48, 12)))

    sid = "threat-package-installs"
    objects.append(saved_search(sid, "Package Install Commands", idx,
        ["timestamp", "endpoint_hostname", "agent_name", "payload.command"],
        "action_type:command_exec AND (payload.command:\"npm install\" OR payload.command:\"yarn add\" OR payload.command:\"pnpm add\" OR payload.command:\"pip install\" OR payload.command:\"poetry add\" OR payload.command:\"cargo add\" OR payload.command:\"go get\" OR payload.command:\"gem install\")"))
    panels.append((sid, "search", "Package Install Commands", (0, 34, 24, 12)))

    sid = "threat-network-exfil"
    objects.append(saved_search(sid, "Network Exfiltration Indicators", idx,
        ["timestamp", "endpoint_hostname", "agent_name", "payload.command"],
        "action_type:command_exec AND (payload.command:\"curl -X POST\" OR payload.command:\"curl --data\" OR payload.command:\"wget --post\")"))
    panels.append((sid, "search", "Network Exfiltration Indicators", (24, 34, 24, 12)))

    sid = "threat-ci-modifications"
    objects.append(saved_search(sid, "Build/CI Command Modifications", idx,
        ["timestamp", "endpoint_hostname", "agent_name", "tool_name", "payload.command", "payload.path"],
        "(action_type:command_exec AND (payload.command:docker OR payload.command:make OR payload.command:gradle OR payload.command:mvn)) OR (action_type:file_write AND payload.path:*workflows*)"))
    panels.append((sid, "search", "Build/CI Command Modifications", (0, 46, 48, 12)))

    vid = "threat-after-hours"
    objects.append(saved_visualization(vid, "After-Hours Activity",
        json.dumps({
            "title": "After-Hours Activity",
            "type": "histogram",
            "aggs": [
                {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
                {"id": "2", "enabled": True, "type": "date_histogram", "schema": "segment", "params": {
                    "field": "timestamp", "interval": "h", "min_doc_count": 0, "extended_bounds": {},
                }},
                {"id": "3", "enabled": True, "type": "terms", "schema": "group", "params": {
                    "field": "endpoint_hostname", "size": 10, "order": "desc", "orderBy": "1",
                }},
            ],
            "params": {
                "type": "histogram", "addTooltip": True, "addLegend": True, "legendPosition": "right",
                "categoryAxes": [{"id": "CategoryAxis-1", "type": "category", "position": "bottom", "show": True, "labels": {"show": True, "filter": True, "truncate": 100}}],
                "valueAxes": [{"id": "ValueAxis-1", "name": "LeftAxis-1", "type": "value", "position": "left", "show": True, "labels": {"show": True}}],
                "seriesParams": [{"show": True, "type": "histogram", "mode": "stacked", "data": {"label": "Count", "id": "1"}, "valueAxis": "ValueAxis-1"}],
            },
        }), idx))
    panels.append((vid, "visualization", "After-Hours Activity", (0, 58, 24, 12)))

    sid = "threat-sensitive-tools"
    objects.append(saved_search(sid, "Sensitive Tool Usage", idx,
        ["timestamp", "endpoint_hostname", "agent_name", "tool_name", "payload.command"],
        "tool_name:WebFetch OR tool_name:WebSearch OR tool_name:mcp__*"))
    panels.append((sid, "search", "Sensitive Tool Usage", (24, 58, 24, 12)))

    vid = "threat-failed-commands"
    objects.append(saved_visualization(vid, "Failed Commands (High Frequency)",
        make_vis_state_bar("Failed Commands (High Frequency)", "endpoint_hostname", 20), idx,
        search_source={"index": idx, "query": {"query": "result_status:error AND action_type:command_exec", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "Failed Commands (High Frequency)", (0, 70, 48, 12)))

    dashboard = saved_dashboard("threat-detection", "Threat Detection", panels)
    objects.append(dashboard)
    write_ndjson("threat-detection.ndjson", objects)


# ============================================================
# Agent Activity Dashboard
# ============================================================
def build_agent_activity():
    objects = []
    panels = []
    idx = INDEX_PATTERN_ID

    vid = "agent-sessions-pie"
    objects.append(saved_visualization(vid, "Sessions per Agent",
        json.dumps({
            "title": "Sessions per Agent",
            "type": "pie",
            "aggs": [
                {"id": "1", "enabled": True, "type": "cardinality", "schema": "metric", "params": {"field": "session_id", "customLabel": "Sessions"}},
                {"id": "2", "enabled": True, "type": "terms", "schema": "segment", "params": {"field": "agent_name", "size": 10, "order": "desc", "orderBy": "1"}},
            ],
            "params": {"type": "pie", "addTooltip": True, "addLegend": True, "legendPosition": "right", "isDonut": True, "labels": {"show": True, "values": True, "last_level": True, "truncate": 100}},
        }), idx))
    panels.append((vid, "visualization", "Sessions per Agent", (0, 0, 20, 12)))

    vid = "agent-tool-usage"
    objects.append(saved_visualization(vid, "Tool Usage Distribution",
        make_vis_state_hbar("Tool Usage Distribution", "tool_name", 20), idx))
    panels.append((vid, "visualization", "Tool Usage Distribution", (20, 0, 28, 12)))

    vid = "agent-file-writes-repo"
    objects.append(saved_visualization(vid, "File Writes by Repository",
        make_vis_state_bar("File Writes by Repository", "working_directory", 10), idx,
        search_source={"index": idx, "query": {"query": "action_type:file_write", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "File Writes by Repository", (0, 12, 24, 12)))

    vid = "agent-commands-by-agent"
    objects.append(saved_visualization(vid, "Commands by Agent",
        make_vis_state_bar("Commands by Agent", "agent_name", 6, "payload.command.keyword"), idx,
        search_source={"index": idx, "query": {"query": "action_type:command_exec", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "Commands by Agent", (24, 12, 24, 12)))

    vid = "agent-most-modified"
    objects.append(saved_visualization(vid, "Most Modified Files",
        make_vis_state_table("Most Modified Files", [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {"customLabel": "Modifications"}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "bucket", "params": {"field": "payload.path", "size": 20, "order": "desc", "orderBy": "1", "customLabel": "File Path"}},
        ]), idx,
        search_source={"index": idx, "query": {"query": "action_type:file_write", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "Most Modified Files", (0, 24, 24, 14)))

    vid = "agent-mcp-tools"
    objects.append(saved_visualization(vid, "MCP Tool Usage",
        make_vis_state_table("MCP Tool Usage", [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {"customLabel": "Invocations"}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "bucket", "params": {"field": "tool_name", "size": 20, "order": "desc", "orderBy": "1", "customLabel": "Tool"}},
        ]), idx,
        search_source={"index": idx, "query": {"query": "tool_name:mcp__* OR tool_name:WebFetch OR tool_name:WebSearch", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "MCP Tool Usage", (24, 24, 24, 14)))

    vid = "agent-heatmap"
    objects.append(saved_visualization(vid, "Activity Heatmap (Endpoints x Time)",
        make_vis_state_heatmap("Activity Heatmap (Endpoints x Time)"), idx))
    panels.append((vid, "visualization", "Activity Heatmap (Endpoints x Time)", (0, 38, 48, 14)))

    vid = "agent-events-over-time"
    objects.append(saved_visualization(vid, "Events per Agent Over Time",
        json.dumps({
            "title": "Events per Agent Over Time",
            "type": "area",
            "aggs": [
                {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {}},
                {"id": "2", "enabled": True, "type": "date_histogram", "schema": "segment", "params": {
                    "field": "timestamp", "interval": "auto", "min_doc_count": 1, "extended_bounds": {},
                }},
                {"id": "3", "enabled": True, "type": "terms", "schema": "group", "params": {
                    "field": "agent_name", "size": 6, "order": "desc", "orderBy": "1",
                }},
            ],
            "params": {
                "type": "area", "grid": {"categoryLines": False},
                "categoryAxes": [{"id": "CategoryAxis-1", "type": "category", "position": "bottom", "show": True, "labels": {"show": True, "filter": True, "truncate": 100}}],
                "valueAxes": [{"id": "ValueAxis-1", "name": "LeftAxis-1", "type": "value", "position": "left", "show": True, "labels": {"show": True}}],
                "addTooltip": True, "addLegend": True, "legendPosition": "right",
                "seriesParams": [{"show": True, "type": "area", "mode": "stacked", "data": {"label": "Count", "id": "1"}, "valueAxis": "ValueAxis-1"}],
            },
        }), idx))
    panels.append((vid, "visualization", "Events per Agent Over Time", (0, 52, 48, 12)))

    dashboard = saved_dashboard("agent-activity", "Agent Activity", panels)
    objects.append(dashboard)
    write_ndjson("agent-activity.ndjson", objects)


# ============================================================
# Endpoint Health Dashboard
# ============================================================
def build_endpoint_health():
    objects = []
    panels = []
    idx = INDEX_PATTERN_ID

    vid = "health-reporting-endpoints"
    objects.append(saved_visualization(vid, "Reporting Endpoints (24h)",
        make_vis_state_metric("Reporting Endpoints (24h)", "cardinality", "endpoint_hostname", "Endpoints"), idx))
    panels.append((vid, "visualization", "Reporting Endpoints (24h)", (0, 0, 16, 5)))

    vid = "health-total-endpoints"
    objects.append(saved_visualization(vid, "Total Endpoints (7d)",
        make_vis_state_metric("Total Endpoints (7d)", "cardinality", "endpoint_hostname", "Endpoints"), idx))
    panels.append((vid, "visualization", "Total Endpoints (7d)", (16, 0, 16, 5)))

    vid = "health-unique-agents"
    objects.append(saved_visualization(vid, "Unique Agents Active",
        make_vis_state_metric("Unique Agents Active", "cardinality", "agent_name", "Agents"), idx))
    panels.append((vid, "visualization", "Unique Agents Active", (32, 0, 16, 5)))

    vid = "health-last-seen"
    objects.append(saved_visualization(vid, "Last Seen per Endpoint",
        make_vis_state_table("Last Seen per Endpoint", [
            {"id": "1", "enabled": True, "type": "max", "schema": "metric", "params": {"field": "timestamp", "customLabel": "Last Seen"}},
            {"id": "3", "enabled": True, "type": "count", "schema": "metric", "params": {"customLabel": "Event Count"}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "bucket", "params": {"field": "endpoint_hostname", "size": 50, "order": "asc", "orderBy": "1", "customLabel": "Endpoint"}},
        ]), idx))
    panels.append((vid, "visualization", "Last Seen per Endpoint", (0, 5, 48, 14)))

    vid = "health-events-per-endpoint"
    objects.append(saved_visualization(vid, "Events per Endpoint",
        make_vis_state_bar("Events per Endpoint", "endpoint_hostname", 20), idx))
    panels.append((vid, "visualization", "Events per Endpoint", (0, 19, 48, 12)))

    vid = "health-agent-coverage"
    objects.append(saved_visualization(vid, "Agent Coverage per Endpoint",
        make_vis_state_table("Agent Coverage per Endpoint", [
            {"id": "1", "enabled": True, "type": "count", "schema": "metric", "params": {"customLabel": "Events"}},
            {"id": "2", "enabled": True, "type": "terms", "schema": "bucket", "params": {"field": "endpoint_hostname", "size": 25, "order": "desc", "orderBy": "1", "customLabel": "Endpoint"}},
            {"id": "3", "enabled": True, "type": "terms", "schema": "bucket", "params": {"field": "agent_name", "size": 10, "order": "desc", "orderBy": "1", "customLabel": "Agent"}},
        ]), idx))
    panels.append((vid, "visualization", "Agent Coverage per Endpoint", (0, 31, 48, 14)))

    vid = "health-export-gaps"
    objects.append(saved_visualization(vid, "Event Ingest Timeline (Gaps = Missing Exports)",
        make_vis_state_line("Event Ingest Timeline"), idx))
    panels.append((vid, "visualization", "Event Ingest Timeline (Gaps = Missing Exports)", (0, 45, 48, 10)))

    vid = "health-error-rate"
    objects.append(saved_visualization(vid, "Error Rate per Endpoint",
        make_vis_state_bar("Error Rate per Endpoint", "endpoint_hostname", 20), idx,
        search_source={"index": idx, "query": {"query": "result_status:error", "language": "kuery"}, "filter": []}))
    panels.append((vid, "visualization", "Error Rate per Endpoint", (0, 55, 48, 12)))

    dashboard = saved_dashboard("endpoint-health", "Endpoint Health", panels, time_from="now-7d")
    objects.append(dashboard)
    write_ndjson("endpoint-health.ndjson", objects)


if __name__ == "__main__":
    print("Building dashboard NDJSON files...")
    build_soc_overview()
    build_threat_detection()
    build_agent_activity()
    build_endpoint_health()
    print("Done.")
