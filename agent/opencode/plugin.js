import { execFileSync } from "child_process";
import { appendFileSync } from "fs";

export const GryphPlugin = async ({ directory }) => {
  const debugFilePath = process.env.GRYPH_OPENCODE_DEBUG_FILE_PATH || "";

  function invokeGryph(hookType, payload) {
    if (debugFilePath) {
      try {
        const line = JSON.stringify({ ts: new Date().toISOString(), hookType, payload });
        appendFileSync(debugFilePath, line + "\n");
      } catch (_) {}
    }
    try {
      execFileSync("__GRYPH_COMMAND__", ["_hook", "opencode", hookType], {
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

  return {
    "tool.execute.before": async (input, output) => {
      invokeGryph("tool.execute.before", {
        hook_type: "tool.execute.before",
        session_id: input.sessionID,
        tool: input.tool,
        args: output.args,
        cwd: directory,
      });
    },
    "tool.execute.after": async (input, output) => {
      try {
        invokeGryph("tool.execute.after", {
          hook_type: "tool.execute.after",
          session_id: input.sessionID,
          tool: input.tool,
          result: {
            title: output.title,
            output: output.output,
            metadata: output.metadata,
          },
          cwd: directory,
        });
      } catch (_) {}
    },
    event: async ({ event }) => {
      const type = event.type;
      if (!["session.created", "session.idle", "session.error"].includes(type))
        return;
      let sessionId = "";
      if (type === "session.created") {
        sessionId = event.properties?.info?.id || "";
      } else {
        sessionId = event.properties?.sessionID || "";
      }
      try {
        invokeGryph(type, {
          hook_type: type,
          properties: { sessionId, ...event.properties },
          cwd: directory,
        });
      } catch (_) {}
    },
  };
};
