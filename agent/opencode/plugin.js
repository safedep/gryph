import { execFileSync } from "child_process";
import { appendFileSync } from "fs";

// One file serves OpenCode 1.x and 2.x, so the install does not need the
// OpenCode version. OpenCode 1.3.4 and later read the default export: 1.x
// calls server() and 2.x calls setup(). OpenCode 1.3.3 and earlier call every
// export as a function. They run GryphPlugin and log an error for the
// default export.

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

// A subagent session has a parent. Its prompt comes from the model, not from
// the user. A failed lookup cannot tell the two apart, so Gryph must not
// record the prompt as user intent.
async function parentOf(lookup) {
  try {
    const session = await lookup();
    if (!session) return { parentID: "", lookupFailed: true };
    return { parentID: session.parentID || "", lookupFailed: false };
  } catch (_) {
    return { parentID: "", lookupFailed: true };
  }
}

function promptPayload(sessionID, parent, prompt, directory) {
  return {
    hook_type: "chat.message",
    session_id: sessionID,
    parent_session_id: parent.parentID,
    parent_lookup_failed: parent.lookupFailed,
    prompt,
    cwd: directory,
  };
}

// OpenCode 1.x returns one object of hooks from a function.
async function server({ directory, client }) {
  return {
    "chat.message": async (input, output) => {
      const prompt = (output.parts || [])
        .filter((part) => part.type === "text" && !part.synthetic)
        .map((part) => part.text)
        .join("\n");
      if (!prompt) return;
      const parent = await parentOf(async () => (await client.session.get({ path: { id: input.sessionID } }))?.data);
      invokeGryph("chat.message", promptPayload(input.sessionID, parent, prompt, directory));
    },
    "tool.execute.before": async (input, output) => {
      invokeGryph("tool.execute.before", {
        hook_type: "tool.execute.before",
        session_id: input.sessionID,
        call_id: input.callID,
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
          call_id: input.callID,
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
}

// V2 returns model-facing tool output as content parts. Gryph stores a
// string, so join the text parts.
function textContent(content) {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return undefined;
  const text = content
    .map((part) => (part?.type === "text" ? part.text : ""))
    .filter(Boolean)
    .join("\n");
  return text || undefined;
}

// OpenCode 2.x events that Gryph records, by the hook type that Gryph
// parses. An execution is one turn of a session, as session.idle is in 1.x.
const sessionEvents = {
  "session.created": "session.created",
  "session.execution.succeeded": "session.idle",
  "session.execution.interrupted": "session.idle",
  "session.execution.failed": "session.error",
};

// OpenCode 2.x registers each hook on the domain that owns it.
async function setup(ctx) {
  const directory = ctx.location.directory;

  // Subscribe before the hooks are registered. OpenCode creates the session
  // right after setup returns, so a later subscription misses
  // session.created.
  const events = new AbortController();
  (async () => {
    for await (const event of ctx.event.subscribe({ types: Object.keys(sessionEvents), signal: events.signal })) {
      const hookType = sessionEvents[event.type];
      if (!hookType) continue;
      const properties = { sessionId: event.data?.sessionID || "" };
      if (hookType === "session.error") {
        properties.message = event.data?.error?.message || String(event.data?.error ?? "session failed");
      }
      try {
        invokeGryph(hookType, { hook_type: hookType, properties, cwd: directory });
      } catch (_) {}
    }
  })().catch(() => {});

  await ctx.session.hook("prompt", async (event) => {
    const prompt = event.prompt?.text || "";
    if (!prompt) return;
    const parent = await parentOf(() => ctx.session.get({ sessionID: event.sessionID }));
    invokeGryph("chat.message", promptPayload(event.sessionID, parent, prompt, directory));
  });

  // Only execute.before can stop a call. OpenCode turns the thrown error
  // into a tool error.
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
      // A failed call has a status and an error instead of a result. Keep
      // the status, so Gryph records the failure when the error is empty.
      const result =
        event.status === "completed"
          ? {
              output:
                typeof output === "string"
                  ? output
                  : textContent(content) ?? (output === undefined ? undefined : JSON.stringify(output)),
              content,
              metadata,
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

  return () => events.abort();
}

export const GryphPlugin = server;

export default { id: "gryph", server, setup };
