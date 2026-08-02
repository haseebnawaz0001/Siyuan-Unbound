# CLAUDE.md — SiYuan

Instructions for coding agents working in this repository. The working method assumed throughout is: plan before changing anything → state explicitly which subagents the work needs and why → execute → verify with real output → don't stop until the goal is met.

`AGENTS.md` is the authoritative repository guide (layout, toolchain, cross-repo notes, conventions) and this file does not restate it. What follows is only what that working method means **here** — what counts as verification in this repo, where the work splits cleanly, and the rules that must not be broken.

This fork diverges from upstream SiYuan in four ways that will catch you out if you assume otherwise; `docs/FORK.md` records them.

## Verification in this repo — what "verified" actually means

The plan's verification step must use these, because the usual defaults are **forbidden here**:

| Change | Verification | Never |
|---|---|---|
| Frontend (`app/src/**`) | `cd app && pnpm run lint` (runs `tsc` typecheck + eslint) | Never *verify* by building — no `pnpm build`, `npx webpack` or `pnpm dev` as a check. Building is on explicit request only; see below. |
| Frontend with tests | `cd app && pnpm test` (`node --import tsx --test`) | — |
| Kernel (`kernel/**`) | `go test ./...` in the relevant package; `gofmt -w` after every edit | Never *verify* by compiling; building is on explicit request only, see below. Never restart a kernel the developer is running. |
| Sync engine (`third_party/dejavu/**`) | `gofmt -l .`, `go vet ./...`, `go test -count=1 ./...` **run from inside `third_party/dejavu`** — it is a separate Go module, so verifying from `kernel/` proves nothing. `test/sync` is a two-client simulation; keep it green | Do not revert the `replace` in `kernel/go.mod` — it is deliberate and permanent. |
| i18n (`app/appearance/langs/*.json`) | `python scripts/check-lang-keys.py` (exit 0 required) | — |

If a change cannot be verified by any of the above, say so explicitly in the completion report rather than implying it was checked.

### Building the app

Building is **on explicit request only** — never to check my own work, never as an unasked-for final step. When asked, build it without further negotiation:

```bash
cd app && pnpm run build                                                  # four webpack bundles → app/stage/build/
cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" .
```

Two preconditions, every time. **Check nothing is already running** (`ps aux | grep -iE "webpack|pnpm|electron|SiYuan-Kernel"`) — a concurrent `pnpm dev` writes the same output tree and `pnpm build` will corrupt it; if one is running, report that and stop. And **never kill a kernel the developer is running** — building the binary is fine, restarting their process is not.

CGO is required, so a C compiler must be on `PATH`. Where there is no system gcc, `zig cc` works — point `CC` and `CXX` at it. `docs/BUILD.md` has the full matrix, including the ways the repo's five build paths differ from each other.

## Subagent allocation — natural split lines in SiYuan

This codebase decomposes cleanly. Use these boundaries when planning the fan-out:

- **Kernel vs frontend** — `kernel/` (Go) and `app/src/` (TypeScript) are independent surfaces for most features. Two agents, parallel.
- **i18n** — a string change means **21 files** in `app/appearance/langs/`. Always its own agent, and it must carry the full rule set: new keys at the **top** of the object; `_kernel` entries appended at the **end** with the next incremental numeric key; each language **genuinely translated**, never copy-pasted across files; ASCII `...` for ellipses, never `…`/`……`; `ld246.com` only in `zh-CN.json`, `liuyun.io` everywhere else.
- **Tracing a feature end-to-end** — `app/src/util/fetch.ts` → `kernel/api/router.go::ServeAPI` (~400 endpoints) → `kernel/model/`. This reads many files to produce a short answer: give it to a read-only `Explore` agent rather than doing it inline.
- **Independent kernel packages** — `av/`, `sql/`, `search/`, `bazaar/`, `mcp/`, `treenode/` rarely share mutable state within a single task. Safe to parallelize.
- **Do not parallelize** edits to the same file or the same language-file set; that is a serial slice.

Model tiers for these slices — cheap models for breadth, expensive ones for depth, and never downgrade the agent that reconciles the others' output: the i18n agent and endpoint tracing run fine on **Sonnet 5**; locating call sites across `app/src/` or `kernel/` is **Haiku 4.5** work — but keep its 200K context in mind, since `kernel/model/` files are large. Reserve **Opus 5** for the kernel's genuinely hard surfaces: `av/` value/filter semantics, `sql/` index queues and FTS, sync/`dejavu` interactions, and the `mcp/`+`model/auth.go` privilege boundary.

## Hard constraints the plan must respect

1. **Never `git commit` / `git push` unless explicitly asked** — no exceptions. This holds even when the goal reads like "ship X".
2. **Never hand-edit**: `app/stage/protyle/js/lute/lute.min.js` (built upstream from `88250/lute`), `app/stage/build/**`, `app/src/types/dist/**`, `app/changelogs/**`, `app/kernel/SiYuan-Kernel*`, `*.syso`, `kernel/kernel.aar`, `app/pandoc/*`.
3. **Never commit a local `replace` directive** in `kernel/go.mod` — it points at a path that exists only on one machine, so it breaks builds for everyone else. Temporary ones for local dependency testing must be reverted before the task is reported complete. **The committed `replace` for dejavu is the one exception and is permanent** — it points inside the repository, so it resolves for every clone and in CI. Do not revert it; see `docs/FORK.md` §4.
4. **Comments in English**, wrapped at 120 characters, describing what the code does — not what it replaced. This fork diverges from upstream here, which writes them in Chinese, so expect comment conflicts on every rebase.
5. **TypeScript**: semicolons, double quotes, space indent. **Go**: `gofmt`.
6. **Markdown**: no hand-wrapping — one line per paragraph, table row, or list item.
7. **Icons**: reuse from `app/appearance/icons/litheness/icon.js`; do not hand-write SVG.
8. **GitHub work**: prefer the `gh` CLI.
9. **Windows scripting**: Node.js or Python, not PowerShell.
