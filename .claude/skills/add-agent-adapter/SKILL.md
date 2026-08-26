---
name: add-agent-adapter
description: Use when adding support for a new AI coding agent to Gryph, or when changing how an existing agent adapter is wired. Trigger this whenever the user wants to integrate a new agent (for example Claude Code, Cursor, Gemini, Codex, Windsurf, or any other tool), add a new adapter package under agent/, wire agent hook events, or asks how adapters detect, install hooks, parse events, or register. Also trigger on phrases like "add a new agent", "support agent X", "new adapter", or "hook up agent X to gryph", even when the user does not say the word "adapter".
---

# Add a New Agent Adapter

The full, current guide is `docs/agent-adapter.md`. It is the single source of truth for this task. Read it and follow it step by step.

Do not add an agent adapter from memory. The adapter pattern touches 13 or more files across `agent/`, `config/`, `cli/`, and `tui/`. A missed file breaks detection, registration, or the security response path. The doc lists every file and the exact change each one needs.

## How to use this skill

1. Read `docs/agent-adapter.md` in full.
2. Follow each step in order. Use the wiring table in the doc to change every listed file.
3. Use an existing adapter as your reference. The `agent/gemini/` package is the recommended model for the `settings.json` pattern.
4. Run the verify commands from the doc's final step before you finish.

## Keep the doc correct

The doc is the source of truth, so keep it accurate. If you find the code no longer matches the doc, update `docs/agent-adapter.md` in the same change. Do not fork the guidance into this skill.
