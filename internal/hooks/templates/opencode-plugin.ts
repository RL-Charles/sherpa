// Sherpa ticket tracking for OpenCode. Installed by `shp hooks install`.
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

    // Tag the agent's own shell commands so `shp` knows an agent is calling.
    await ctx.shell.hook("create.before", (event: any) => {
      event.env.SHERPA_ACTOR = "agent:opencode"
    })

    // Session start: fetch Sherpa's context once per session and add it to
    // every model request (V2 request edits are not persisted).
    await ctx.session.hook("context", async (event: any) => {
      const id = String(event.sessionID ?? "")
      if (!context.has(id)) context.set(id, (await shp("session-start", {})).out)
      const text = context.get(id)
      if (text) event.system.push({ type: "text", text })
    })

    // Heartbeat for the Sherpa TUI's live "working" indicator.
    await ctx.session.hook("prompt", () => {
      void shp("working", {})
    })

    await ctx.tool.hook("execute.before", async (event: any) => {
      if (!EDIT_TOOLS.has(event.tool)) {
        void shp("working", {})
        return
      }
      const r = await shp("pre-edit", { files: filesOf(directory, event.tool, event.input) })
      if (r.code === 2) throw new Error(r.out)
    })

    await ctx.tool.hook("execute.after", async (event: any) => {
      if (event.status !== "completed") return
      if (event.tool === "shell") {
        // Link pull requests the agent opens with gh.
        const command = String(event.input?.command ?? "")
        if (command.includes("gh pr")) await shp("post-shell", { command, output: textOf(event.result) })
        return
      }
      if (!EDIT_TOOLS.has(event.tool)) return
      await shp("post-edit", { files: filesOf(directory, event.tool, event.input) })
    })

    // Before a session goes idle, let Sherpa ask the agent to log or submit.
    // A synthetic message admitted here runs in the same execution.
    await ctx.session.hook("experimental.idle", async (event: any) => {
      const id = event.sessionID
      const s = await ctx.session.get({ sessionID: id }).catch(() => undefined)
      if (s?.parentID ?? s?.data?.parentID) return // subagent sessions
      const count = nudged.get(id) ?? 0
      const r = await shp("stop", { stopActive: count > 0 })
      if (r.code === 2 && r.out && count < 2) {
        nudged.set(id, count + 1)
        await ctx.session.synthetic({ sessionID: id, text: r.out })
      } else {
        nudged.delete(id)
      }
    })
  },
}
