# AI features — Reference

What the AI in this fork can do, what it can change, and where your data goes.

Everything here is verified against source. Where a claim matters — what a tool can mutate, what leaves the machine — the file it comes from is named so you can check rather than trust.

**No AI feature costs money.** There is no `IsSubscriber` or `needSubscribe` check anywhere in `kernel/conf/ai.go`, `kernel/api/ai.go`, `kernel/api/agent.go`, `kernel/agent/` or `kernel/mcp/`. Every provider, the agent, vision, image generation, embeddings, rerank and MCP are free and point at endpoints you configure.

**Nothing happens until you configure a provider.** `NewAI()` starts with an empty provider list, the editing path is gated on `HasAnyProvider()`, embedding defaults to off, and the agent reports "No model configured". No AI network request occurs out of the box.

---

## 0. There are two AI systems, not one

They are commonly confused and behave completely differently.

| | Inline editor AI | Agent |
|---|---|---|
| Where | `kernel/model/ai.go` | `kernel/agent/` |
| What it is | One request, one response | A tool-calling loop that runs until the model stops asking for tools |
| Invoked by | Block menu, `/` slash command, shortcut | The Agent dock panel |
| Sees | The blocks you selected, plus up to `maxHistoryMessages` prior turns | Whatever it decides to read via 32 tools |
| Can change your notes | No | **Yes** |
| State | One process-wide context cache | Sessions persisted to disk |

The rest of this document is mostly about the agent, because it is the part with teeth.

---

## 1. Providers

Any OpenAI-compatible endpoint works — the protocol is the contract, not the vendor. `Provider.BaseURL` defaults to `https://api.openai.com/v1` and is freely overridable, so Ollama, LM Studio, vLLM, llama.cpp, OpenRouter, DeepSeek and Azure-compatible proxies all work. A **keyless** provider is explicitly supported and tested, which is how local servers are meant to be wired in.

Several providers can coexist, and each scenario picks its own model: `agent`, `editing`, `vision`, `imageGeneration`, `embedding`, `rerank`.

Configured in `Settings → AI`, stored under `ai` in `conf/conf.json`, schema in `kernel/conf/ai.go`.

| Group | Notable fields |
|---|---|
| `providers[]` | `baseURL`, `apiKey`, `protocol` (only `openai` is honoured), `requestTimeout` (1–600s, default 120), `models[]` |
| `agent` | `maxToolCallRounds` (default 64), `streamIdleTimeout` (default 120s), `sessionTimeout` (default 600s, `0` = unlimited), `maxRetries` (default 3) |
| `editing` | `maxHistoryMessages` (default 7), `temperature`, `maxCompletionTokens` |
| `vision` | `maxImageBytes` (default 20 MiB), `maxPixels` (40 MP), `maxEdge` (2048px) |
| `imageGeneration` | `size`, `quality`, `outputFormat` (`png`/`jpeg`/`webp`) |
| `embedding` | `enabled` (**default off**), `apiKey`, `baseURL`, `dimensions` |
| `rerank` | `enabled`, `endpoint` (full URL — paths differ per vendor), `candidateCount` (default 30) |
| `webSearch` | `enabled` (**default off**), `exaApiKey` — see §4 |
| `mcp.servers[]` | External MCP servers, `stdio` or `http` |

`streamIdleTimeout` bounds the gap *between* chunks, not the whole response, so a slow but steady stream is never cut off.

---

## 2. What the agent can do

32 tools, in `kernel/mcp/tools/`. Grouped by what they can actually change:

**Read-only** — `search`, `sql`, `outline`, `ref`, `system`, `workspace`, `question`, `todo_write`

**Changes your notes** — `block`, `document`, `notebook`, `dailynote`, `attr`, `database`, `tag`, `bookmark`, `template`, `history` (including `rollback`), `repo`, `import`, `asset`, `unzip`, `skill`, `sync`, `export`

**Filesystem** — `file`

**Leaves the machine** — `web_fetch`, `web_search`, `http_request`, `image`

**Drives your UI** — `frontend`, plus any action a plugin registered

Two deserve precision, because they are the ones worth worrying about.

### `sql` is genuinely read-only

`kernel/mcp/tools/sql.go:75` calls `sql.CheckReadonlyStatement` and rejects anything that is not a read. This is enforced, not advisory.

### `file` is confined to the workspace, and not much else

`kernel/mcp/tools/file.go` resolves every path under `util.WorkspaceDir` and refuses to leave it, including after symlink resolution (`file.go:91,100`). It refuses encrypted-notebook directories (`file.go:96`) and blocks `conf/conf.json` specifically, because that file holds credentials (`file.go:106`).

Within those bounds it can `write`, `delete`, `rename` and `copy` **any** file — plugin code, other notebooks' `.sy` files, snapshots — bypassing the block model entirely. The system prompt tells the model not to use it for workspace data, but that is an instruction, not enforcement. The hard control is the confirmation dialog.

---

## 3. Consent: what asks before it acts

Each tool declares its effects per action (`ActionEffects` in `kernel/mcp/tools/types.go`). `needsConfirm` in `kernel/agent/agent.go` checks those first: an action declaring `LocalWrite`, `DataEgress` or `ExternalCost` prompts, showing the tool's arguments as pretty-printed JSON — so for `web_fetch` you see the exact URL before approving.

Tools that write locally also trigger a data-repository snapshot first, so an unwanted change can be rolled back.

A short allowlist of always-safe tools skips the prompt: `question`, `todo_write`, `search`, `sql`, `web_search`. The first four cannot change anything or reach the network; `web_search` is there because its control is the opt-in setting rather than a per-call prompt (§4).

---

## 4. Where your data goes

| Feature | What is sent | To |
|---|---|---|
| Inline editor AI | The blocks you selected, plus recent turns | Your configured provider |
| Agent | The full conversation, every tool call and result | Your configured provider |
| `image.analyze` / `generate` | The image, base64-encoded | Your vision/image provider — **confirms first** |
| `search.semantic` | The query embedding | Your embedding provider — **confirms first** |
| Embedding indexer | **Every block in the workspace** between 7 and 12,000 characters, continuously | Your embedding provider. Off by default; vectors are stored locally in SQLite |
| `web_fetch` | A GET to a URL the model chose | Wherever it points — **confirms first, URL shown** |
| `web_search` | The query text | **Exa (`mcp.exa.ai`), a third party** — off by default, see below |
| `http_request` | Arbitrary method, headers, body | Wherever you or the model point it; non-GET confirms |
| MCP servers | Tool-call arguments | Servers you configured yourself |

Everything except `web_search` goes to an endpoint you chose.

### `web_search` and Exa

This tool queries [Exa](https://exa.ai), which is **not** your configured AI provider. It is therefore **off by default** and, while off, is withheld from the tool list entirely — neither the built-in agent nor an external MCP client is told it exists, so no model can call it. Enabling it in `Settings → AI → Web search` is the consent; it does not then prompt per call.

An Exa API key is optional. Without one the request is anonymous. It is stored like every other key (§6).

---

## 5. What is kept on disk

**Agent sessions**, as plaintext JSON under `<workspace>/data/storage/ai/agent/sessions/` (`kernel/agent/session.go:43`). Each holds the full transcript: your messages, the model's, and every tool call *and its result* — which can include whole documents, search results and fetched web pages. They persist indefinitely and survive restarts.

**Embeddings**, in SQLite, if you enable semantic search. Local only; the vectors are computed remotely but not stored remotely.

Prompts and responses are **not** written to `siyuan.log`.

---

## 6. Known issues

Recorded because they are real, not because they are about to change.

**"Session Allow" is all-or-nothing.** When a tool asks for confirmation, the third button grants standing permission — but it sets a single global flag. `needsConfirm` reads a per-tool key (`alwaysAllow[tool+"::"+action]`, `kernel/agent/agent.go:1139`), yet nothing outside the tests ever writes one; only `"*"` is ever set. So clicking it to wave through a web fetch also stops the agent asking before it **deletes a document** for the rest of that session. The button's own tooltip says as much — "Don't ask again for any operations in this session" — but the consequence is easy to miss. Prefer approving individually.

**API keys are obfuscated, not encrypted.** Keys in `conf.json` are AES-CBC encrypted with a key and IV hardcoded in the source: `kernel/util/crypt.go:29,45`. Anyone who can read the file can decrypt it using the public repository. This is adequate against shoulder-surfing and nothing else. Treat `conf.json` as a secret.

**Agent transcripts are unencrypted and unbounded.** See §5. If a session read sensitive documents, that content sits in plaintext under your workspace until you delete the session.

**Compaction loses information permanently.** When a conversation exceeds the model's context window, older turns are replaced with a mechanical digest — first 300 characters of each user message, first sentence of each reply, a tally of tool names. Tool arguments and results are discarded, and because the persisted transcript is rewritten too, the loss is permanent for that session.

---

## See also

- [`FORK.md`](./FORK.md) — how this fork differs from upstream, including why `web_search` changed
- [`SYNC.md`](./SYNC.md) — sync to your own storage
- [`WORKSPACE.md`](./WORKSPACE.md) — what lives on disk
