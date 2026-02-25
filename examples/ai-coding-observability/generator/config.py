"""Generator configuration: endpoints, agents, and activity patterns."""

OPENSEARCH_URL = "http://localhost:9200"
INDEX_PREFIX = "gryph-events"

# --- Endpoint simulation ---

ENDPOINTS = [
    {"hostname": "dev-mbp-001", "username": "agarcia",   "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-002", "username": "bchen",     "os": "darwin", "team": "frontend"},
    {"hostname": "dev-mbp-003", "username": "cjohnson",  "os": "darwin", "team": "platform"},
    {"hostname": "dev-mbp-004", "username": "dkim",      "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-005", "username": "ewilson",   "os": "darwin", "team": "frontend"},
    {"hostname": "dev-mbp-006", "username": "fzhang",    "os": "darwin", "team": "data"},
    {"hostname": "dev-mbp-007", "username": "gpatel",    "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-008", "username": "hlee",      "os": "darwin", "team": "platform"},
    {"hostname": "dev-mbp-009", "username": "imartinez", "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-010", "username": "jnguyen",   "os": "darwin", "team": "frontend"},
    {"hostname": "dev-linux-011", "username": "kbrown",  "os": "linux",  "team": "infra"},
    {"hostname": "dev-linux-012", "username": "ltaylor", "os": "linux",  "team": "infra"},
    {"hostname": "dev-mbp-013", "username": "mwhite",   "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-014", "username": "nharris",  "os": "darwin", "team": "frontend"},
    {"hostname": "dev-mbp-015", "username": "orobinson","os": "darwin", "team": "data"},
    {"hostname": "dev-mbp-016", "username": "pclark",   "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-017", "username": "qlewis",   "os": "darwin", "team": "platform"},
    {"hostname": "dev-mbp-018", "username": "rwalker",  "os": "darwin", "team": "frontend"},
    {"hostname": "dev-mbp-019", "username": "syoung",   "os": "darwin", "team": "backend"},
    {"hostname": "dev-mbp-020", "username": "tallen",   "os": "darwin", "team": "data"},
]

AGENTS = [
    {"name": "claude-code", "weight": 0.40, "versions": ["1.0.16", "1.0.17", "1.0.18"]},
    {"name": "cursor",      "weight": 0.25, "versions": ["0.48.1", "0.48.2", "0.49.0"]},
    {"name": "gemini",      "weight": 0.15, "versions": ["2.1.0", "2.2.0"]},
    {"name": "windsurf",    "weight": 0.10, "versions": ["1.5.0", "1.6.0"]},
    {"name": "opencode",    "weight": 0.05, "versions": ["0.3.0", "0.4.0"]},
    {"name": "openclaw",    "weight": 0.05, "versions": ["0.1.0", "0.2.0"]},
]

# --- Activity patterns ---

SESSIONS_PER_DAY_MEAN = 6
SESSIONS_PER_DAY_STD = 2

EVENTS_PER_SESSION_MEAN = 45
EVENTS_PER_SESSION_STD = 20

WORK_HOURS_START = 9
WORK_HOURS_END = 18
OFF_HOURS_PROBABILITY = 0.05

ACTION_WEIGHTS = {
    "command_exec": 0.30,
    "file_read":    0.25,
    "file_write":   0.20,
    "tool_use":     0.15,
    "notification": 0.10,
}

RESULT_STATUS_WEIGHTS = {
    "success": 0.92,
    "error":   0.08,
}
