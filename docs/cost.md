# Cost & Token Usage Tracking

Gryph tracks token usage and estimates costs for AI coding agent sessions. Cost data is collected automatically from agent transcript files and computed using bundled model pricing.

## How It Works

1. When an agent session ends, the hook process reads the session transcript as the user who runs the agent. It extracts per-model token usage (input, output, cache read, cache write).
2. The hook process matches the usage against bundled pricing data (sourced from [models.dev](https://models.dev)) to estimate the cost in USD.
3. The hook process sends the totals to the decision service with the session end event. The service stores them on the session and does not open the transcript itself. It records the cost source as `client_reported:transcript`, because it did not verify the totals.
4. `gryph cost` and `gryph sessions` report the stored cost data.

`gryph cost --sync` reads the transcript again in the `cost` command and records the cost source as `transcript`.

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

Cost data is collected automatically at session end. No configuration is required. The `--sync` flag is only needed to backfill older sessions or recompute after a pricing update.

## Pricing Data

Model pricing is bundled in `pricing/models.json`, sourced from the models.dev API. To update:

```bash
make update-pricing
```

The pricing provider resolves model IDs using layered matching: exact match, date suffix stripping (e.g., `claude-sonnet-4-20250514` to `claude-sonnet-4`), and provider prefix lookup.

## Supported Agents

| Agent       | Transcript Parsing | Status    |
| ----------- | ------------------ | --------- |
| Claude Code | JSONL transcripts  | Supported |
| Cursor      | TBD                | Planned   |