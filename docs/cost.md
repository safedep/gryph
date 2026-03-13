# Cost & Token Usage Tracking

Gryph tracks token usage and estimates costs for AI coding agent sessions. Cost data is collected automatically from agent transcript files and computed using bundled model pricing.

## How It Works

1. When an agent session ends, Gryph parses the session transcript to extract per-model token usage (input, output, cache read, cache write).
2. Usage is matched against bundled pricing data (sourced from [models.dev](https://models.dev)) to estimate cost in USD.
3. Cost data is stored on the session and queryable via `gryph cost`.

Sessions that use multiple models (e.g., Sonnet for edits, Opus for planning) get per-model breakdowns.

## Commands

```bash
# View cost summary for all sessions
gryph cost

# Today's costs
gryph cost --today

# Last 7 days, grouped by model
gryph cost --since "1w" --by model

# Group by day for trend analysis
gryph cost --since "30d" --by day

# Group by agent
gryph cost --by agent

# Filter by agent or model
gryph cost --agent claude-code
gryph cost --model opus

# Backfill cost data for sessions missing it
gryph cost --sync

# Force recompute all cost data
gryph cost --sync --force
```

## Automatic Collection

Cost data is collected automatically at session end — no configuration required. The `--sync` flag is only needed to backfill older sessions or recompute after a pricing update.

## Pricing Data

Model pricing is bundled in `pricing/models.json`, sourced from the models.dev API. To update:

```bash
make update-pricing
```

The pricing provider resolves model IDs using layered matching: exact match, date suffix stripping (e.g., `claude-sonnet-4-20250514` → `claude-sonnet-4`), and provider prefix lookup.

## Supported Agents

| Agent       | Transcript Parsing | Status    |
| ----------- | ------------------ | --------- |
| Claude Code | JSONL transcripts  | Supported |
| Cursor      | TBD                | Planned   |