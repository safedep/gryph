import { execFileSync } from "child_process";
import { join } from "path";
import { homedir } from "os";
import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";

const PLUGIN_ID = "gryph";
const transcriptPath = join(homedir(), ".openclaw", "logs", "commands.log");

function resolveSessionId(event, ctx) {
  return event.sessionId || ctx.sessionKey || "";
}

function invokeGryph(hookType, payload) {
  try {
    execFileSync("__GRYPH_COMMAND__", ["_hook", "openclaw", hookType], {
      input: JSON.stringify(payload),
      stdio: ["pipe", "pipe", "pipe"],
      timeout: 5000,
    });
  } catch (e) {
    if (e.status === 2) {
      throw new Error(e.stderr?.toString()?.trim() || "Blocked by gryph");
    }
  }
}

export default definePluginEntry({
  id: PLUGIN_ID,
  name: "Gryph",
  description: "Audit trail plugin for OpenClaw agent actions",
  register(api) {
    api.on("before_tool_call", (event, ctx) => {
      invokeGryph("before_tool_call", {
        hook_type: "before_tool_call",
        tool: event.toolName,
        args: event.params,
        session_id: resolveSessionId(event, ctx),
        agent_id: ctx.agentId,
        transcript_path: transcriptPath,
      });
    });

    api.on("after_tool_call", (event, ctx) => {
      try {
        invokeGryph("after_tool_call", {
          hook_type: "after_tool_call",
          tool: event.toolName,
          args: event.params,
          result: event.result,
          error: event.error,
          duration_ms: event.durationMs,
          session_id: resolveSessionId(event, ctx),
          agent_id: ctx.agentId,
          transcript_path: transcriptPath,
        });
      } catch (_) {}
    });

    api.on("session_start", (event, ctx) => {
      try {
        invokeGryph("session_start", {
          hook_type: "session_start",
          session_id: resolveSessionId(event, ctx),
          agent_id: ctx.agentId,
          transcript_path: transcriptPath,
        });
      } catch (_) {}
    });

    api.on("session_end", (event, ctx) => {
      try {
        invokeGryph("session_end", {
          hook_type: "session_end",
          session_id: resolveSessionId(event, ctx),
          message_count: event.messageCount,
          duration_ms: event.durationMs,
          agent_id: ctx.agentId,
          transcript_path: transcriptPath,
        });
      } catch (_) {}
    });
  },
});
