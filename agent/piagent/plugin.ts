import { spawn, spawnSync } from "node:child_process";

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
    const result = sendToGryphWithExitCode("tool_call", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
      tool_name: event.toolName,
      tool_call_id: event.toolCallId,
      input: event.input,
    });

    // Exit code 2 means block - propagate blocking to Pi Agent
    if (result.exitCode === 2) {
      return {
        block: true,
        reason: result.stderr || "Blocked by security policy",
      };
    }

    // Exit code 1 means error - allow but could log (for now, just allow)
    // Exit code 0 means allow
  });

  pi.on("tool_result", async (event, ctx) => {
    // Skip tool_result for file operations - tool_call already has all the info
    // This prevents duplicate events and incorrect stats
    if (event.toolName === "write" || event.toolName === "edit" || event.toolName === "read") {
      return;
    }

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

// sendToGryphWithExitCode waits for the gryph hook to complete and returns the exit code.
// This is required for tool_call hooks where blocking decisions (exit code 2) must be enforced.
function sendToGryphWithExitCode(hookType: string, data: Record<string, unknown>): {
  exitCode: number;
  stderr: string;
} {
  const payload = JSON.stringify({
    hook_event_name: hookType,
    ...data,
    timestamp: new Date().toISOString(),
  });

  // Use spawnSync for synchronous execution to get exit code reliably
  // This ensures we block tool execution until the hook decision is received
  const result = spawnSync("__GRYPH_COMMAND__", ["_hook", "pi-agent", hookType], {
    input: payload,
    stdio: ["pipe", "pipe", "pipe"],
  });

  return { exitCode: result.status ?? 0, stderr: result.stderr?.toString() ?? "" };
}
