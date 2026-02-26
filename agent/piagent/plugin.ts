import { spawn } from "node:child_process";

export default function (pi: ExtensionAPI) {
  pi.on("session_start", async (event, ctx) => {
    sendToGryph("session_start", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
    });
  });

  pi.on("session_shutdown", async (event, ctx) => {
    sendToGryph("session_shutdown", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
    });
  });

  pi.on("tool_call", async (event, ctx) => {
    sendToGryph("tool_call", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
      tool_name: event.toolName,
      tool_call_id: event.toolCallId,
      input: event.input,
    });
  });

  pi.on("tool_result", async (event, ctx) => {
    sendToGryph("tool_result", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
      tool_name: event.toolName,
      tool_call_id: event.toolCallId,
      input: event.input,
      content: event.content,
      is_error: event.isError,
    });
  });
}

function sendToGryph(hookType: string, data: Record<string, unknown>) {
  const payload = JSON.stringify({
    hook_event_name: hookType,
    ...data,
    timestamp: new Date().toISOString(),
  });

  const child = spawn("__GRYPH_COMMAND__", ["_hook", "pi-agent", hookType], {
    stdio: ["pipe", "pipe", "pipe"],
  });

  child.stdin.write(payload);
  child.stdin.end();

  // Fire-and-forget: silently ignore errors
}
