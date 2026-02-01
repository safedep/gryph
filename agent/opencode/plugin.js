import { execFileSync } from "child_process";

export const GryphPlugin = async ({ directory }) => {
  function invokeGryph(hookType, payload) {
    try {
      execFileSync("gryph", ["_hook", "opencode", hookType], {
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
    tool: {
      execute: {
        before: async (input, output) => {
          invokeGryph("tool.execute.before", {
            hook_type: "tool.execute.before",
            tool: input.tool,
            args: output.args,
            cwd: directory,
          });
        },
        after: async (input, output) => {
          try {
            invokeGryph("tool.execute.after", {
              hook_type: "tool.execute.after",
              tool: input.tool,
              args: output.args,
              cwd: directory,
            });
          } catch (_) {}
        },
      },
    },
    event: async ({ event }) => {
      const type = event.type;
      if (
        ["session.created", "session.idle", "session.error"].includes(type)
      ) {
        try {
          invokeGryph(type, {
            hook_type: type,
            properties: event.properties || {},
            cwd: directory,
          });
        } catch (_) {}
      }
    },
  };
};
