<p align="center">
<img alt="SiYuan" src="app/stage/icon.png" width="128">
<br><br>
<a href="https://www.gnu.org/licenses/agpl-3.0.txt"><img src="https://img.shields.io/badge/license-AGPLv3-blue.svg" alt="License: AGPL v3"></a>
</p>

<p align="center">
<strong>English</strong> | <a href="README.zh-CN.md">中文</a> | <a href="README.ja.md">日本語</a> | <a href="README.tr.md">Türkçe</a>
</p>

---

## What this is

**SiYuan Unbound is a fork of [SiYuan](https://github.com/siyuan-note/siyuan)**, a privacy-first personal knowledge management system with block-level references and Markdown WYSIWYG. It is not a rewrite — almost all of the code is upstream's, and the fork is kept close enough to keep merging from it.

Four things differ:

- **Sync works without a subscription, for storage you supply.** S3-compatible object storage, WebDAV and a local filesystem directory all sync with no account. SiYuan's own hosted services stay gated — see below.
- **Telemetry is removed.** No hardware device fingerprint, no automatic announcement pull.
- **English is the default language**, and the source comments are in English. The language switcher still works and every translation still ships.
- **Conflicting documents merge block by block** instead of one side always losing.

[`docs/FORK.md`](docs/FORK.md) records each divergence, why it was made, and where a rebase against upstream will conflict.

This is an unofficial fork. It is not supported by, endorsed by, or affiliated with the SiYuan project, and it has no app-store listings, no published Docker image and no support channel. Licence is unchanged: AGPL-3.0.

![Editing and block references](screenshots/feature0.png)

![Database views](screenshots/feature5-1.png)

## Features

- Content block
  - Block-level reference and two-way links
  - Custom attributes
  - SQL query embed
  - Protocol `siyuan://`
- Editor
  - Block-style
  - Markdown WYSIWYG
  - List outline
  - Block zoom-in
  - Million-word large document editing
  - Mathematical formulas, charts, flowcharts, Gantt charts, timing charts, staves, etc.
  - Web clipping
  - PDF Annotation link
- Export
  - Block ref and embed
  - Standard Markdown with assets
  - PDF, Word and HTML
  - Copy to WeChat MP, Zhihu and Yuque
- Database
  - Table view
- Flashcard spaced repetition
- AI writing and Q/A chat via OpenAI API
- Tesseract OCR
- Multi-tab, drag and drop to split screen
- Template snippet
- JavaScript/CSS snippet
- Sync to your own S3 / WebDAV / local storage, no account required
- Docker deployment
- Community marketplace

### What still costs money

Everything that runs on SiYuan's own servers, deliberately left gated: official cloud sync, cloud image and asset hosting, cloud reminders, CDN asset rewriting on export, and Liandi publishing. AGPL-3.0 covers modifying and running your own copy; it does not cover free use of someone else's infrastructure. See [Pricing](https://b3log.org/siyuan/en/pricing.html) if you want those.

## Getting a build

There are no downloads. Build it yourself.

```bash
git clone git@github.com:haseebnawaz0001/Siyuan-Unbound.git
cd Siyuan-Unbound

cd app && pnpm install && pnpm run install:electron && pnpm run build && cd ..

cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..

./app/kernel/SiYuan-Kernel serve
```

You need Go 1.26, Node 24, pnpm 11.12.0 and a C compiler. The kernel serves the frontend over HTTP, so that last line is a working install — no installer required.

For desktop installers, cross-compilation, Docker, mobile, and the ways the five build paths in this repo disagree with each other, read **[`docs/BUILD.md`](docs/BUILD.md)**.

### Or let CI build it

`.github/workflows/cd.yml` has no owner gate, so it runs on a fork. Push a tag matching `*-alpha*`, `*-beta*` or `*-rc*`, or trigger the workflow manually, and it builds Windows, macOS and Linux installers and attaches them to a Release on your own repository.

Two caveats: the Android job in that workflow pushes to a repository you do not control and will fail — the desktop artifacts are unaffected — and this path has not been exercised on this fork.

## Self-hosting

Build the image and run it:

```bash
docker build -t siyuan-unbound .
```

This fork publishes no image and cannot: `.github/workflows/dockerimage.yml` is gated on the upstream owner, so its publish job never runs here. **[`docs/DEPLOY.md`](docs/DEPLOY.md)** covers running the container, Docker Compose, Unraid and TrueNAS, and an important caveat — the Docker build omits the `sqlcipher` build tag, so encrypted notebooks should not be assumed to work in it.

## Documentation

| Document | What it answers |
|---|---|
| [FORK.md](docs/FORK.md) | How this fork differs from upstream, and why |
| [SYNC.md](docs/SYNC.md) | Setting up sync against your own S3, WebDAV or local storage |
| [AI.md](docs/AI.md) | What the AI can do, what it can change, and where your data goes |
| [BUILD.md](docs/BUILD.md) | Building from source, every platform |
| [DEPLOY.md](docs/DEPLOY.md) | Docker, Compose, Unraid, TrueNAS |
| [WORKSPACE.md](docs/WORKSPACE.md) | What a workspace looks like on disk |
| [SY-FORMAT.md](docs/SY-FORMAT.md) | The `.sy` document format |
| [ENCRYPTED-NOTEBOOK.md](docs/ENCRYPTED-NOTEBOOK.md) | How encrypted notebooks work |
| [API.md](docs/API.md) | The kernel HTTP API |
| [CONTRIBUTING.md](.github/CONTRIBUTING.md) | Development setup and conventions |
| [AGENTS.md](AGENTS.md) | Repository guide, including the rules a coding agent must follow here |

## Command-line interface

The kernel binary is also the CLI, and it reads workspace data directly — no running server required.

```bash
# List all notebooks
siyuan notebook list -w ~/SiYuan

# Full-text search with JSON output
siyuan search "keyword" -w ~/SiYuan -f json

# Search inside asset files (PDF/Word/Excel/txt etc.)
siyuan search "phrase" --asset -w ~/SiYuan
siyuan search "phrase" --asset --ext pdf --ext docx -w ~/SiYuan

# Export a document as Markdown
siyuan export md --id <block-id> -w ~/SiYuan
```

| Category | Commands |
|----------|----------|
| Notebooks & Documents | `notebook`, `document`, `dailynote` — CRUD and daily notes |
| Content | `block`, `attr`, `outline` — block read/write, attributes, outline |
| Metadata | `tag`, `bookmark`, `template` — tags, bookmarks, template snippets |
| Queries | `search`, `sql` — full-text, semantic, asset-content, and SQL queries |
| References | `ref` — backlinks and mentions |
| Import/Export | `export`, `import`, `inbox` — Markdown, HTML, preview, Word, .sy.zip, Data, cloud inbox |
| Data Management | `repo`, `history`, `sync` — snapshots, versions, cloud sync |
| Utilities | `asset`, `file` — resources and file system |
| Database | `database` — attribute view management |
| Server | `serve` — start the kernel HTTP server |
| Workspace & System | `workspace`, `system` — list, inspect, system info |

Run `siyuan --help` for the full command tree. Use `-f json` (default is `-f table`) for script-friendly output. Most mutating commands also support `--dry-run` to preview changes without applying them.

The binary is `<install-dir>/resources/kernel/SiYuan-Kernel`, or wherever you built it. To reach it as `siyuan`, symlink it onto your `PATH`:

```bash
ln -s <install-dir>/resources/kernel/SiYuan-Kernel /usr/local/bin/siyuan
```

## Architecture and ecosystem

| Project | Role |
|---|---|
| [lute](https://github.com/88250/lute) | Editor engine — the Markdown/`.sy` AST |
| [dejavu](third_party/dejavu) | Data repo and sync engine — **vendored fork**, see [FORK.md](docs/FORK.md) §4 |
| [riff](https://github.com/siyuan-note/riff) | Spaced-repetition scheduler |
| [bazaar](https://github.com/siyuan-note/bazaar) | Community marketplace |
| [petal](https://github.com/siyuan-note/petal) | Plugin API |
| [chrome](https://github.com/siyuan-note/siyuan-chrome) | Web clipper extension |
| [android](https://github.com/siyuan-note/siyuan-android) / [ios](https://github.com/siyuan-note/siyuan-ios) / [harmony](https://github.com/siyuan-note/siyuan-harmony) | Mobile apps wrapping the gomobile kernel |

Apart from `dejavu`, these are upstream-maintained projects that this fork consumes unchanged. The mobile apps and the extension talk to the kernel over its HTTP API, so they work against this fork, but building them is out of scope here — see [`docs/BUILD.md`](docs/BUILD.md) §6.

## FAQ

### How does SiYuan store data?

The data is saved in the workspace data folder:

- `assets` is used to save all inserted assets
- `emojis` is used to save emoji images
- `snippets` is used to save code snippets
- `storage` is used to save query conditions, layouts and flashcards, etc.
- `templates` is used to save template snippets
- `widgets` is used to save widgets
- `plugins` is used to save plugins
- `public` is used to save public data
- The rest of the folders are the notebook folders created by the user, files with the suffix of `.sy` in the notebook folder are used to save the document data, and the data format is JSON

[`docs/WORKSPACE.md`](docs/WORKSPACE.md) is the full reference.

### Does it support data synchronization through a third-party sync disk?

No — pointing Dropbox, OneDrive or a similar folder-sync tool at your workspace can corrupt data.

Connecting your own S3, WebDAV or local filesystem storage is a different thing entirely and is fully supported, with no subscription. The kernel writes an immutable, content-addressed repository rather than syncing live files. See [`docs/SYNC.md`](docs/SYNC.md).

### Is this open source?

Yes, AGPL-3.0, same as upstream. Note that AGPL §13 applies if you run it as a network service for others — you owe them the source.

Upstream's own repositories, all separate projects: [user interface and kernel](https://github.com/siyuan-note/siyuan), [Android](https://github.com/siyuan-note/siyuan-android), [iOS](https://github.com/siyuan-note/siyuan-ios), [HarmonyOS](https://github.com/siyuan-note/siyuan-harmony), [Chrome clipping extension](https://github.com/siyuan-note/siyuan-chrome).

### How do I upgrade?

There is no auto-update — that mechanism points at upstream's release infrastructure, which this fork does not have. Pull and rebuild:

```bash
git pull
cd app && pnpm install && pnpm run build && cd ..
cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..
```

To pick up upstream's changes as well, merge them first: `git fetch upstream && git merge upstream/master`. Expect conflicts on comments — [`docs/FORK.md`](docs/FORK.md) lists where they land.

### What if some blocks (such as paragraph blocks in list items) cannot find the block icon?

The block icon is omitted for the first sub-block under the list item. You can move the cursor into this block and trigger its block menu with <kbd>Ctrl+/</kbd>.

### What should I do if the data repo key is lost?

- If the data repo key is correctly initialized on multiple devices previously, the key is the same on all devices and can be retrieved in <kbd>Settings</kbd> - <kbd>Account & Sync</kbd> - <kbd>Local Data Repo</kbd> - <kbd>Data repo key</kbd> - <kbd>Copy key string</kbd>
- If it has not been configured correctly before (for example, the keys on multiple devices are inconsistent) or all devices are unavailable and the key string cannot be obtained, you can reset the key by following the steps below:

  1. Manually back up the data, you can use <kbd>Export Data</kbd> or directly copy the <kbd>workspace/data/</kbd> folder on the file system
  2. <kbd>Settings</kbd> - <kbd>Account & Sync</kbd> - <kbd>Local Data Repo</kbd> - <kbd>Data repo key</kbd> - <kbd>Reset data repo</kbd>
  3. Reinitialize the data repo key. After initializing the key on one device, other devices import the key
  4. The cloud uses the new synchronization directory, the old synchronization directory is no longer available and can be deleted
  5. The existing cloud snapshots are no longer available and can be deleted

## Acknowledgement

SiYuan is the work of [b3log](https://github.com/siyuan-note) and its contributors. This fork exists because that work is good and open; all credit for the software belongs upstream, and any bug introduced by the changes in [`docs/FORK.md`](docs/FORK.md) belongs here, not to them. Please do not report issues with this fork to the upstream project.

SiYuan depends on many open source projects — see `kernel/go.mod` and `app/package.json`.
