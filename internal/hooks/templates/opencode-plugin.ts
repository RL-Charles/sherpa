// Sherpa ticket tracking for OpenCode. Installed by `shp hooks install`.
// Calls `shp hook <event> --agent opencode`; exit code 2 means block/continue.
import type { Plugin } from "@opencode-ai/plugin"

const EDIT_TOOLS = new Set(["edit", "write", "apply_patch"])

const filesOf = (tool: string, args: any): string[] =>
  tool === "apply_patch"
    ? [...String(args?.patchText ?? "").matchAll(/^\*\*\* (?:Add|Update|Delete) File: (.+)$/gm)].map((m) => m[1].trim())
    : args?.filePath
      ? [String(args.filePath)]
      : []

export const SherpaPlugin: Plugin = async ({ $, client, directory }) => {
  const contextSent = new Set<string>()
  const nudged = new Map<string, number>()

  const shp = async (event: string, payload: object) => {
    try {
      const r = await $`shp hook ${event} --agent opencode`
        .cwd(directory)
        .env({ ...process.env, SHERPA_HOOK_PAYLOAD: JSON.stringify({ dir: directory, ...payload }) })
        .quiet()
        .nothrow()
      return { code: r.exitCode, out: r.stdout.toString().trim() }
    } catch {
      return { code: 0, out: "" } // shp missing or broken: never block the agent
    }
  }

  return {
    // Tag the agent's own shell commands so `shp` knows an agent is calling.
    "shell.env": async (_input, output) => {
      output.env.SHERPA_ACTOR = "agent:opencode"
    },

    // Session start: add Sherpa's context to the system prompt once per session.
    "experimental.chat.system.transform": async (input, output) => {
      const id = input.sessionID ?? ""
      if (contextSent.has(id)) return
      contextSent.add(id)
      const r = await shp("session-start", {})
      if (r.out) output.system.push(r.out)
    },

    // Heartbeat for the Sherpa TUI's live "working" indicator.
    "chat.message": async () => {
      await shp("working", {})
    },

    "tool.execute.before": async (input, output) => {
      if (!EDIT_TOOLS.has(input.tool)) {
        await shp("working", {})
        return
      }
      const r = await shp("pre-edit", { files: filesOf(input.tool, output.args) })
      if (r.code === 2) throw new Error(r.out)
    },

    "tool.execute.after": async (input, output) => {
      const args = (input as any).args
      if (input.tool === "bash") {
        // Link pull requests the agent opens with gh.
        if (String(args?.command ?? "").includes("gh pr"))
          await shp("post-shell", { command: String(args.command), output: String((output as any)?.output ?? "") })
        return
      }
      if (!EDIT_TOOLS.has(input.tool)) return
      await shp("post-edit", { files: filesOf(input.tool, args) })
    },

    event: async ({ event }) => {
      if (event.type !== "session.idle") return
      const id = (event as any).properties?.sessionID
      if (!id) return
      const s = await client.session.get({ path: { id } }).catch(() => undefined)
      if (s?.data?.parentID) return // subagent sessions
      const count = nudged.get(id) ?? 0
      const r = await shp("stop", { stopActive: count > 0 })
      if (r.code === 2 && r.out && count < 2) {
        nudged.set(id, count + 1)
        await client.session.promptAsync({ path: { id }, body: { parts: [{ type: "text", text: r.out }] } })
      } else {
        nudged.delete(id)
      }
    },
  }
}
