// Sherpa ticket tracking for OpenCode. Installed by `shp hooks install` into
// OpenCode's global plugins, so it loads in every directory; `shp hook` does
// nothing where it finds no .sherpa board.
// Calls `shp hook <event> --agent opencode`; exit code 2 means block/continue.
// OpenCode V2 plugin API: https://opencode.ai/v2/docs/build/plugins/server/
// It has no runtime imports besides Node built-ins, so it loads without a
// package install in .opencode/.
import { execFile } from "node:child_process"
import { isAbsolute, resolve } from "node:path"

// V2 tool names: edit and write take `path`; patch takes `patchText`.
const EDIT_TOOLS = new Set(["edit", "write", "patch"])

const filesOf = (dir: string, tool: string, input: any): string[] => {
  const paths =
    tool === "patch"
      ? [...String(input?.patchText ?? "").matchAll(/^\*\*\* (?:Add|Update|Delete) File: (.+)$/gm)].map((m) => m[1].trim())
      : input?.path
        ? [String(input.path)]
        : []
  return paths.map((p) => (isAbsolute(p) ? p : resolve(dir, p)))
}

// Text of a tool result: a string, or text parts of a content array.
const textOf = (result: any): string => {
  const content = result?.content ?? result?.output ?? ""
  if (typeof content === "string") return content
  if (Array.isArray(content)) return content.map((c: any) => (c?.type === "text" ? c.text : "")).join("\n")
  return ""
}

export default {
  id: "sherpa",
  async setup(ctx: any) {
    const directory: string = ctx.location.directory
    const context = new Map<string, string>()
    const nudged = new Map<string, number>()

    const shp = (event: string, payload: object) =>
      new Promise<{ code: number; out: string }>((done) => {
        execFile(
          "shp",
          ["hook", event, "--agent", "opencode"],
          {
            cwd: directory,
            timeout: 30_000,
            env: { ...process.env, SHERPA_HOOK_PAYLOAD: JSON.stringify({ dir: directory, ...payload }) },
          },
          (err: any, stdout) => {
            // shp missing or broken: never block the agent
            if (err && typeof err.code !== "number") return done({ code: 0, out: "" })
            done({ code: err ? err.code : 0, out: String(stdout ?? "").trim() })
          },
        )
      })

    // Tag the agent's own shell commands so `shp` knows the actor and session.
    await ctx.shell.hook("create.before", (event: any) => {
      event.env.SHERPA_ACTOR = "agent:opencode"
      const sid = String(event.sessionID ?? event.env.OPENCODE_SESSION_ID ?? "")
      if (sid) {
        event.env.SHERPA_AGENT_SESSION = sid
        if (!event.env.SHERPA_SESSION) event.env.SHERPA_SESSION = `opencode-${sid}`
      }
    })

    // Session context: fetch Sherpa's resume brief and keep it fresh after
    // ticket switches or state updates (`shp start`, `shp handoff`, `shp submit`, etc.).
    await ctx.session.hook("context", async (event: any) => {
      const id = String(event.sessionID ?? "")
      if (!context.has(id)) context.set(id, (await shp("session-start", { session_id: id })).out)
      const text = context.get(id)
      if (text) event.system.push({ type: "text", text })
    })

    // Heartbeat for the Sherpa TUI's live "working" indicator.
    await ctx.session.hook("prompt", (event: any) => {
      void shp("working", { session_id: String(event?.sessionID ?? "") })
    })

    await ctx.tool.hook("execute.before", async (event: any) => {
      const sid = String(event.sessionID ?? "")
      if (!EDIT_TOOLS.has(event.tool)) {
        void shp("working", { session_id: sid, tool: String(event.tool ?? "") })
        return
      }
      const files = filesOf(directory, event.tool, event.input)
      const r = await shp("pre-edit", { session_id: sid, tool: String(event.tool ?? ""), files })
      if (r.code === 2) throw new Error(r.out)
    })

    await ctx.tool.hook("execute.after", async (event: any) => {
      if (event.status !== "completed") return
      const sid = String(event.sessionID ?? "")
      if (event.tool === "shell") {
        const command = String(event.input?.command ?? "")
        // Refresh cached context whenever the agent mutates Sherpa state.
        if (/\b(?:shp|sherpa)\s+(?:start|submit|done|handoff|now|log|check|deliverable|where|status|review)\b/.test(command)) {
          context.delete(sid)
        }
        // Link pull requests the agent opens with gh.
        if (command.includes("gh pr")) {
          await shp("post-shell", { session_id: sid, tool: "shell", command, output: textOf(event.result) })
          context.delete(sid)
        }
        return
      }
      if (!EDIT_TOOLS.has(event.tool)) return
      await shp("post-edit", { session_id: sid, tool: String(event.tool ?? ""), files: filesOf(directory, event.tool, event.input) })
    })

    // Before a session goes idle, let Sherpa ask the agent to log, handoff or submit.
    // A synthetic message admitted here runs in the same execution.
    await ctx.session.hook("experimental.idle", async (event: any) => {
      const id = event.sessionID
      const s = await ctx.session.get({ sessionID: id }).catch(() => undefined)
      if (s?.parentID ?? s?.data?.parentID) return // subagent sessions
      const count = nudged.get(id) ?? 0
      const r = await shp("stop", { session_id: String(id ?? ""), stopActive: count > 0 })
      if (r.code === 2 && r.out && count < 2) {
        nudged.set(id, count + 1)
        await ctx.session.synthetic({ sessionID: id, text: r.out })
      } else {
        nudged.delete(id)
      }
    })
  },
}
