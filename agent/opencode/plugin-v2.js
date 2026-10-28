import { execFileSync } from "child_process";
import { appendFileSync } from "fs";

// OpenCode 2.x plugin.
//
// The V1 plugin.js returns one object of hooks from a function. OpenCode 2.x
// loads a default export with an id and a setup function, and registers each
// hook on the domain that owns it. Keep plugin.js for OpenCode 1.x.
export default {
  id: "gryph",
  async setup(ctx) {
    const directory = ctx.location.directory;
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

    // V2 returns model-facing tool output as content parts. Gryph stores a
    // string, so join the text parts before sending the event.
    function textContent(content) {
      if (typeof content === "string") return content;
      if (!Array.isArray(content)) return undefined;
      const text = content
        .map((part) => (part?.type === "text" ? part.text : ""))
        .filter(Boolean)
        .join("\n");
      return text || undefined;
    }

    // A subagent session has a parent. Its prompt comes from the model, not
    // from the user. A failed lookup cannot tell the two apart, so Gryph
    // must not record the prompt as user intent.
    async function parentSession(sessionID) {
      try {
        const res = await ctx.session.get({ sessionID });
        if (!res) return { parentID: "", lookupFailed: true };
        return { parentID: res.parentID || "", lookupFailed: false };
      } catch (_) {
        return { parentID: "", lookupFailed: true };
      }
    }

    // V1 chat.message becomes the V2 prompt hook. The prompt text is already
    // assembled; there are no output.parts to join.
    await ctx.session.hook("prompt", async (event) => {
      const prompt = event.prompt?.text || "";
      if (!prompt) return;
      const parent = await parentSession(event.sessionID);
      invokeGryph("chat.message", {
        hook_type: "chat.message",
        session_id: event.sessionID,
        parent_session_id: parent.parentID,
        parent_lookup_failed: parent.lookupFailed,
        prompt,
        cwd: directory,
      });
    });

    // V1 passed (input, output); V2 passes one mutable event. The tool input
    // is event.input, and the call id is event.id.
    await ctx.tool.hook("execute.before", (event) => {
      invokeGryph("tool.execute.before", {
        hook_type: "tool.execute.before",
        session_id: event.sessionID,
        call_id: event.id,
        tool: event.tool,
        args: event.input,
        cwd: directory,
      });
    });

    await ctx.tool.hook("execute.after", (event) => {
      try {
        const output = event.result?.output;
        const content = event.result?.content;
        const metadata = event.result?.metadata;
        // A failed V2 call reports a status and sometimes an error instead of
        // an output. Keep the status so gryph records the failure even when
        // the runtime sends no message.
        const result =
          event.status === "completed"
            ? {
                // V1 output.output was the model-facing text. V2 keeps that
                // in result.content. Prefer a raw string when the tool
                // returns one, then the joined content parts.
                output:
                  typeof output === "string"
                    ? output
                    : textContent(content) ?? (output === undefined ? undefined : JSON.stringify(output)),
                content,
                metadata,
                // V2 shell calls the exit code "exit" in metadata.
                exitCode: metadata?.exit ?? metadata?.exitCode,
              }
            : { error: event.error, status: event.status };
        invokeGryph("tool.execute.after", {
          hook_type: "tool.execute.after",
          session_id: event.sessionID,
          call_id: event.id,
          tool: event.tool,
          args: event.input,
          result,
          cwd: directory,
        });
      } catch (_) {}
    });

    // V1 exposed one event callback. V2 exposes a subscription. OpenCode
    // emits execution start/success/failure rather than session.created,
    // session.idle and session.error, so map them onto the hook types Gryph
    // already parses.
    const events = new AbortController();
    (async () => {
      for await (const event of ctx.event.subscribe({ signal: events.signal })) {
        let hookType = "";
        const properties = { sessionId: event.data?.sessionID || "" };
        if (event.type === "session.execution.started") {
          hookType = "session.created";
        } else if (event.type === "session.execution.succeeded" || event.type === "session.execution.interrupted") {
          hookType = "session.idle";
        } else if (event.type === "session.execution.failed") {
          hookType = "session.error";
          properties.message = event.data?.error?.message || String(event.data?.error ?? "session failed");
        } else {
          continue;
        }
        try {
          invokeGryph(hookType, { hook_type: hookType, properties, cwd: directory });
        } catch (_) {}
      }
    })().catch(() => {});

    return () => events.abort();
  },
};
