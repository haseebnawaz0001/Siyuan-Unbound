# Building from source — Reference

Everything here is taken from a file in this repository: `.github/workflows/cd.yml` (the release pipeline, and the authority when sources disagree), the three `scripts/*-build.*` helpers, the root `Dockerfile`, and `app/package.json`. Where the repo does not establish something, this document says so rather than guessing.

**Nothing in this document has been run end to end here.** The commands are verified to exist and to agree with CI; they are not observed to succeed. Treat a first build as a first build.

---

## 0. In one sentence

Build the TypeScript frontend with pnpm, build the Go kernel into `app/kernel*/`, then let electron-builder wrap the two into an installer — or skip the wrapper and run the kernel binary on its own.

---

## 1. Prerequisites

| Tool | Version | Where that comes from |
|---|---|---|
| Go | `1.26` | `kernel/go.mod:3` — CI resolves the toolchain from this same line via `go-version-file` (`cd.yml:142`) |
| Node | `24` | `cd.yml:161`. The Docker image builds the frontend on Node 22 instead (`Dockerfile:1`), so the two release paths do not use the same Node |
| pnpm | `11.12.0` | `app/package.json:8` (`packageManager`). CI reads this field rather than hardcoding it (`cd.yml:63-66`) |
| A C compiler | see below | `CGO_ENABLED=1` everywhere; the kernel links SQLite |
| Git | any | the build needs a full clone, not a tarball of `kernel/` — see §7 |

### The C toolchain

CGO is on in every build path, so a C compiler is mandatory.

- **Linux.** `scripts/linux-build.sh:70-102` wants a **musl** cross compiler, not plain gcc: `x86_64-linux-musl-gcc` for amd64, `aarch64-linux-musl-gcc` for arm64. It looks in `~/<target>-cross/bin/`, then `PATH`, then falls back to the system `musl-gcc` only when building for the host's own architecture, and finally downloads a prebuilt toolchain from `musl.cc` into `muslbin/`. If you build with plain `go build` instead of the script, your system gcc is fine — you simply will not get the static-PIE binary the script produces.
- **Windows.** MinGW-w64 gcc on `PATH` (`scripts/win-build.bat:93`). For arm64, see the trap in §7.
- **macOS.** The repo never states this. `scripts/darwin-build.sh` sets no `CC` and performs no toolchain detection, so it relies on whatever `clang` is already on `PATH` — in practice the Xcode Command Line Tools. This is inference, not something the repo documents.
- **Alpine / Docker.** `Dockerfile:30` installs `gcc musl-dev`.

### One thing to know before you start

Three build scripts and several npm scripts route downloads through Chinese mirrors. This is not wrong and it falls through to the real source, but it is worth knowing if you audit network egress:

- `GOPROXY=https://mirrors.aliyun.com/goproxy/,https://goproxy.cn,direct` in `scripts/darwin-build.sh:66`, `scripts/linux-build.sh:66` and `scripts/win-build.bat:100`.
- `ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/` in `app/package.json`'s `install:electron` and every `dist*` script.

Plain `go build` and `pnpm install` do not set either.

---

## 2. Frontend

```bash
cd app
pnpm install
pnpm run install:electron   # fetches the Electron binary; desktop builds only
pnpm run build
```

`pnpm run build` is `pnpm run /build:.*/` (`app/package.json`), a glob that runs all four webpack builds. Each emits its own bundle:

| Script | Config | Output |
|---|---|---|
| `build:app` | `webpack.config.js` | `app/stage/build/app` |
| `build:mobile` | `webpack.mobile.js` | `app/stage/build/mobile` |
| `build:desktop` | `webpack.desktop.js` | `app/stage/build/desktop` |
| `build:export` | `webpack.export.js` | `app/stage/build/export` |

The kernel picks which of the first three to serve from the User-Agent. The fourth is not an app UI — it is the client-side renderer library loaded by exported HTML and the PDF preview window.

For iterating on the frontend use `pnpm run dev` (or `dev:mobile` / `dev:desktop` / `dev:export`), which is the same webpack in development mode and watches. **Do not run `pnpm run build` while a `pnpm dev` is running** — see `AGENTS.md` §5.4.

---

## 3. Kernel

The canonical command, from `cd.yml:198`, with `CGO_ENABLED=1` and `GO111MODULE=on` (`cd.yml:200-205`):

```bash
cd kernel
CGO_ENABLED=1 go build -tags "fts5 sqlcipher" \
  -o "../app/kernel/SiYuan-Kernel" \
  -ldflags "-s -w -X github.com/siyuan-note/siyuan/kernel/util.Mode=prod"
```

Drop the `-ldflags` entirely for a local development build; `.github/CONTRIBUTING.md:54-55` documents exactly that shorter form. Without `-X ...util.Mode=prod` the kernel runs in dev mode.

The two build tags are not optional. `fts5` enables SQLite full-text search, which backs all search in the app. `sqlcipher` backs encrypted notebooks — `kernel/sql/database.go:1863` and `kernel/treenode/blocktree.go:908` derive SQLCipher keys for content and block trees.

### Where the binary has to go

electron-builder picks the kernel up from a directory whose name encodes the target, so the `-o` path matters (`cd.yml` matrix, lines 91-118):

| Target | Output path |
|---|---|
| Windows amd64 | `app/kernel/SiYuan-Kernel.exe` |
| Linux amd64 | `app/kernel-linux/SiYuan-Kernel` |
| macOS amd64 | `app/kernel-darwin/SiYuan-Kernel` |
| macOS arm64 | `app/kernel-darwin-arm64/SiYuan-Kernel` |

The helper scripts add `app/kernel-arm64/` (Windows arm64) and `app/kernel-linux-arm64/`.

### The five build paths do not agree

This matters if you compare a binary you built against a release:

| Source | Command | How it differs |
|---|---|---|
| `cd.yml:198` | `-tags "fts5 sqlcipher"`, `-ldflags "-s -w -X ...Mode=prod"` | **the reference build** |
| `scripts/linux-build.sh:109` | adds `-buildmode=pie` and `-extldflags -static-pie` | no `Mode=prod`, so the binary reports dev mode |
| `scripts/darwin-build.sh:74` | `-ldflags "-s -w"` | no `Mode=prod` |
| `scripts/win-build.bat:126` | `-ldflags "-s -w"` | no `Mode=prod` |
| `Dockerfile:46` | **`-tags fts5` only** | **`sqlcipher` is missing** — see below |

**The Docker image is built without `sqlcipher`.** That is a functional difference, not a cosmetic one: encrypted notebooks depend on it. A Docker-built kernel is not equivalent to a release binary and encrypted notebooks should not be assumed to work in it. This predates the fork; it is documented here rather than changed.

### Cross-compiling

Set `GOOS` and `GOARCH` as CI does (`cd.yml:200-205`) and supply a matching C cross compiler. Cross-compiling with CGO is the hard part; the C toolchain is what will stop you, not Go.

---

## 4. Desktop installers

With the frontend built (§2) and the kernel in the right directory (§3):

```bash
cd app
pnpm run dist-linux          # or: dist, dist-arm64, dist-darwin, dist-darwin-arm64, dist-linux-arm64
```

Each maps to `electron-builder --config electron-builder-<variant>.yml --publish=never`. Output lands in `app/build`, named `siyuan-<version>-<os>.<ext>`.

| Script | Config | Produces |
|---|---|---|
| `dist` | `electron-builder.yml` | Windows `nsis` installer |
| `dist-arm64` | `electron-builder-arm64.yml` | Windows arm64 `nsis` |
| `dist-darwin` / `dist-darwin-arm64` | `electron-builder-darwin*.yml` | macOS `dmg` |
| `dist-linux` / `dist-linux-arm64` | `electron-builder-linux*.yml` | `tar.gz`, `AppImage`, `deb`, `rpm` |

Linux RPM output additionally needs the `rpm` tool installed (`cd.yml:207-209`).

### Or skip the installer entirely

The kernel binary is self-contained — it serves the frontend over HTTP. Build §2 and §3, then:

```bash
./app/kernel/SiYuan-Kernel serve --mode=dev
```

and open the port it reports. `.github/CONTRIBUTING.md:57-58` documents this as the normal development loop. `SiYuan-Kernel` is also the CLI: `kernel/main.go` calls into a Cobra root command (`kernel/cli/cmd/root.go:42`) whose subcommands include `serve`, `search`, `export`, `sql`, `sync` and `workspace`. One binary, many subcommands.

---

## 5. Docker

```bash
docker build -t siyuan-unbound .
```

Run it from the **repository root** — the build context must include `third_party/`, not just `kernel/` (§7). See [`DEPLOY.md`](./DEPLOY.md) for running the image, and note the missing `sqlcipher` tag from §3.

---

## 6. Mobile

Android, from `cd.yml:316`, run inside `kernel/`:

```bash
gomobile bind -tags "fts5 sqlcipher" -ldflags "-s -w" -v -o kernel.aar -target android/arm64 -androidapi 26 ./mobile/
```

Prerequisites named in CI: JDK 21 (`cd.yml:277-281`), Android NDK `28.2.13676358` (`cd.yml:283-302`), and gomobile installed at the commit matching `golang.org/x/mobile` in `kernel/go.mod` (`cd.yml:304-311`).

**This does not produce an APK.** It produces `kernel.aar`, which CI then copies into a checkout of the separate `siyuan-note/siyuan-android` repository and builds there with Gradle (`cd.yml:323-380`). Building the Android app from this repository alone is not possible, and that CI job will fail on a fork because it pushes to a repository you do not control.

iOS is documented only in `.github/CONTRIBUTING.md:60-64` and has no CI job. HarmonyOS (`CONTRIBUTING.md:73-100`) is Linux-only and requires patching the Go standard library by hand.

---

## 7. Traps

**`third_party/` must sit next to `kernel/`.** `kernel/go.mod:230` carries `replace github.com/siyuan-note/dejavu => ../third_party/dejavu`. Build from a full clone and this is automatic. Build from a copy of `kernel/` alone — a sparse checkout, a hand-rolled Dockerfile with `COPY kernel/ .` — and `go mod download` fails with `reading ../third_party/dejavu/go.mod: no such file or directory`. The root `Dockerfile:40` handles this explicitly with `ADD third_party/ /third_party/`; none of the four build scripts guard against it, because they all assume a full clone.

**`third_party/dejavu` is a separate Go module.** `go test ./...` in `kernel/` does not run its tests. Verify it from inside that directory:

```bash
cd third_party/dejavu && gofmt -l . && go vet ./... && go test -count=1 ./...
```

Its `test/sync` package is a two-client sync simulation and is the best regression signal in the repo.

**Windows arm64 will not build as committed.** `scripts/win-build.bat:136` hardcodes `CC="D:/Program Files/llvm-mingw-20240518-ucrt-x86_64/bin/aarch64-w64-mingw32-gcc.exe"` — one developer's absolute path. Install `llvm-mingw` and edit that line.

**`win-build.bat` and CI differ on Windows.** The script copies `app/elevator/elevator-amd64.exe` into the kernel folder and hard-links `siyuan.exe` after packaging (`win-build.bat:156,165,210-215`); the CI job does not show those steps. If a locally built Windows package behaves differently from a released one, start here.

---

## 8. Verifying without building

These are the checks that are safe to run and are what CI would catch:

```bash
cd app && pnpm run lint                 # tsc typecheck + eslint
cd app && pnpm test                     # node --import tsx --test
cd kernel && gofmt -l . && go vet ./...
cd third_party/dejavu && gofmt -l . && go vet ./... && go test -count=1 ./...
```

No workflow in this repo runs `go test`, `go vet` or `gofmt` — CI builds but never tests. Run them yourself.

---

## 9. Releases

`.github/workflows/cd.yml` has **no owner gate**, so it runs on a fork. It triggers on tags matching `*-alpha*`, `*-beta*` or `*-rc*`, and on manual `workflow_dispatch`. Pushing such a tag builds Windows, macOS and Linux installers and attaches them to a GitHub Release on your own repository.

Two caveats. The Android job in that workflow pushes to `siyuan-note/siyuan-android` and will fail for anyone who is not upstream — the desktop artifacts are unaffected. And `.github/workflows/dockerimage.yml` is gated `if: github.repository_owner == 'siyuan-note'` (line 19), so it never runs on a fork and no image is ever published; build your own per §5.

This path has not been exercised on this fork.
