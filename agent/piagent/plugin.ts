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
        reason: (result.stderr && result.stderr.trim()) || "Blocked by security policy",
      };
    }

    if (result.exitCode === 1) {
      console.error(`[gryph] hook error: ${(result.stderr && result.stderr.trim()) || "unknown error"}`);
    }
  });

  pi.on("tool_result", async (event, ctx) => {
    // Skip tool_result for file operations except to capture errors
    // This prevents duplicate events but allows error reporting
    if (event.toolName === "write" || event.toolName === "edit" || event.toolName === "read") {
      // Only send tool_result if there was an error (isError is true)
      if (!event.isError) {
        return;
      }
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

  child.on("error", () => {});
}

// sendToGryphWithExitCode waits for the gryph hook to complete and returns the exit code.
// This is required for tool_call hooks where blocking decisions (exit code 2) must be enforced.
// Fail-open design: if the hook binary is missing, unexecutable, or times out, the tool
// is allowed to proceed. This ensures a broken gryph installation doesn't freeze the agent.
// Exit codes: 0 = allow, 1 = error (allow + log), 2 = block.
function sendToGryphWithExitCode(hookType: string, data: Record<string, unknown>): {
  exitCode: number;
  stderr: string;
} {
  const payload = JSON.stringify({
    hook_event_name: hookType,
    ...data,
    timestamp: new Date().toISOString(),
  });

  // Timeout in milliseconds - prevents agent from freezing if _hook hangs
  const timeoutMs = 30000;

  // Use spawnSync for synchronous execution to get exit code reliably
  // This ensures we block tool execution until the hook decision is received
  const result = spawnSync("__GRYPH_COMMAND__", ["_hook", "pi-agent", hookType], {
    input: payload,
    stdio: ["pipe", "pipe", "pipe"],
    timeout: timeoutMs,
  });

  // Fail-open: if spawnSync fails (ENOENT, EACCES, ETIMEDOUT), allow the tool
  // but log the error so misconfiguration is visible to the user.
  if (result.error) {
    const msg = result.error.code === "ETIMEDOUT"
      ? "hook timed out, defaulting to allow"
      : `hook execution failed (${result.error.code || result.error.message}), defaulting to allow`;
    console.error(`[gryph] ${msg}`);
    return { exitCode: 0, stderr: "" };
  }

  // result.status is guaranteed non-null when result.error is null
  return { exitCode: result.status ?? 0, stderr: result.stderr?.toString() ?? "" };
}
