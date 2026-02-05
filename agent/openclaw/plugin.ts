import { execFileSync } from "child_process";

export default function gryphPlugin(api) {
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

  api.on("before_tool_call", (event, ctx) => {
    invokeGryph("before_tool_call", {
      hook_type: "before_tool_call",
      tool: event.toolName,
      args: event.params,
      session_id: ctx.sessionKey,
      agent_id: ctx.agentId,
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
        session_id: ctx.sessionKey,
        agent_id: ctx.agentId,
      });
    } catch (_) {}
  });

  api.on("session_start", (event, ctx) => {
    try {
      invokeGryph("session_start", {
        hook_type: "session_start",
        session_id: event.sessionId,
        agent_id: ctx.agentId,
      });
    } catch (_) {}
  });

  api.on("session_end", (event, ctx) => {
    try {
      invokeGryph("session_end", {
        hook_type: "session_end",
        session_id: event.sessionId,
        message_count: event.messageCount,
        duration_ms: event.durationMs,
        agent_id: ctx.agentId,
      });
    } catch (_) {}
  });
}
