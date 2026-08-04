# Building from source

Step-by-step per platform. Every build command here is quoted from a file in this repository — `.github/workflows/cd.yml`, the three `scripts/*-build.*` files, the root `Dockerfile`, or `app/package.json`. The **toolchain setup** blocks are the exception and are marked as such: those are ordinary install instructions, not something the repo can vouch for.

**Only the Linux path in this document has been run end to end.** The macOS and Windows sections are assembled from the repo's own scripts and CI, which is strong but is not the same as observed.

---

## What you are building

Two halves:

- **The kernel** — a single Go binary. It is the server, the database, the sync engine and the CLI all at once.
- **The frontend** — four webpack bundles under `app/stage/build/`.

The kernel serves the frontend over HTTP. That means **you do not need an installer to have a working install**: build both halves, run the binary, open the port. Installers exist to wrap the two in an Electron shell with a desktop icon.

---

## Versions

| Tool | Version | Where that comes from |
|---|---|---|
| Go | `1.26` | `kernel/go.mod` |
| Node | `24` | `.github/workflows/cd.yml` |
| pnpm | `11.12.0` | `app/package.json` (`packageManager`) |
| A C compiler | see each platform | CGO is on in every build path |

---

## The short version, any platform

```bash
git clone git@github.com:haseebnawaz0001/Siyuan-Unbound.git
cd Siyuan-Unbound

cd app && pnpm install && pnpm run install:electron && pnpm run build && cd ..
cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" . && cd ..

./app/kernel/SiYuan-Kernel serve
```

Open the port it prints. That is a complete install. Everything below is detail, cross-compilation, and installers.

Both build tags matter. `fts5` is SQLite full-text search, which backs all search in the app; `sqlcipher` backs encrypted notebooks. Drop either and you get a kernel that starts but is quietly missing a feature.

---

## Linux

### Toolchain *(setup commands are not verified against this repo)*

```bash
# Go 1.26 -- distro packages are often older, so prefer the tarball
curl -fsSL https://go.dev/dl/go1.26.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
export PATH=/usr/local/go/bin:$PATH

# Node 24 + pnpm
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash -
sudo apt-get install -y nodejs
npm install -g pnpm@11.12.0

# C compiler, plus the packaging tools -- build-essential brings binutils, which the deb target needs for `ar`
sudo apt-get install -y build-essential rpm
```

No system gcc? `zig cc` works — set `CC` and `CXX` to it. This repository has been built that way.

### Build

```bash
cd app
pnpm install
pnpm run install:electron     # only needed if you want an installer
pnpm run build
cd ../kernel
CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel-linux/SiYuan-Kernel" .
```

Run it directly with `./app/kernel-linux/SiYuan-Kernel serve`, or carry on to an installer.

### Installers

```bash
cd app
pnpm run dist-linux           # amd64
pnpm run dist-linux-arm64     # arm64
```

Produces `tar.gz`, `AppImage`, `deb` and `rpm` in `app/build`, named `siyuan-<version>-linux.<ext>`.

The four targets have different requirements, and electron-builder stops at the first one it cannot build. Observed on a machine with neither `binutils` nor `rpm` installed:

| Target | Needs | Result |
|---|---|---|
| `tar.gz` | nothing extra | built, ~247 MB |
| `AppImage` | nothing extra | built, ~249 MB |
| `deb` | `ar`, from `binutils` | failed: `Need executable 'ar' to convert dir to deb` |
| `rpm` | `rpm` / `rpmbuild` | not reached |

So `sudo apt-get install -y binutils rpm` before packaging, or accept the two targets that need nothing and ignore the failure. CI installs `rpm` explicitly and its runners already carry `binutils`; the local script installs neither.

### What `scripts/linux-build.sh` does differently

Running the script rather than the commands above gets you a **static-PIE** binary:

```bash
go build -buildmode=pie -tags "fts5 sqlcipher" -o "../app/kernel-linux/SiYuan-Kernel" -ldflags "-s -w -extldflags -static-pie" .
```

Neither the macOS nor Windows scripts nor CI does this. For that it wants a **musl** cross compiler — `x86_64-linux-musl-gcc` or `aarch64-linux-musl-gcc`, not plain gcc. `setup_cc()` looks in `~/<target>-cross/bin/`, then `PATH`, then falls back to the system `musl-gcc` when building for the host's own architecture, and finally downloads a toolchain from `musl.cc` into `muslbin/`. That last step reaches out to a third-party host, which is worth knowing before you run it.

### Cross-compiling to arm64

```bash
cd kernel
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-musl-gcc \
  go build -tags "fts5 sqlcipher" -o "../app/kernel-linux-arm64/SiYuan-Kernel" .
```

The C cross compiler is what makes this hard, not Go. `linux-build.sh` will fetch one for you if it is missing.

---

## macOS

### Toolchain *(setup commands are not verified against this repo)*

```bash
xcode-select --install                 # provides clang, which CGO needs
brew install go node
npm install -g pnpm@11.12.0
```

The build scripts never set `CC` on macOS and never check for a compiler, so they assume Xcode Command Line Tools are already present. If clang is missing you get a cgo linker error rather than a useful message.

### Build

```bash
cd app
pnpm install
pnpm run install:electron
pnpm run build
cd ../kernel
CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel-darwin/SiYuan-Kernel" .          # Intel
CGO_ENABLED=1 GOARCH=arm64 go build -tags "fts5 sqlcipher" -o "../app/kernel-darwin-arm64/SiYuan-Kernel" .   # Apple silicon
```

The two architectures go to **different directories**, and electron-builder picks the one matching the config you run.

### Installers

```bash
cd app
pnpm run dist-darwin          # Intel  -> siyuan-<version>-mac.dmg
pnpm run dist-darwin-arm64    # Apple silicon -> siyuan-<version>-mac-arm64.dmg
```

**These dmgs are unsigned.** Upstream's configs referenced an Apple Developer identity, a provisioning profile and an entitlements file — none of which are in this repository, so the target could not build for anyone outside upstream. Both darwin configs now set `identity: null`, which produces an unsigned build that works.

The consequence: macOS Gatekeeper will refuse to open the app on first launch. Right-click → Open, or `xattr -dr com.apple.quarantine /Applications/SiYuan.app`. If you have a Developer ID certificate, set `identity` to its name and restore `provisioningProfile`, `hardenedRuntime` and `entitlements` — the comment in `app/electron-builder-darwin.yml` says exactly which keys.

---

## Windows

### Toolchain *(setup commands are not verified against this repo)*

```powershell
winget install GoLang.Go
winget install OpenJS.NodeJS
npm install -g pnpm@11.12.0
```

You also need **MinGW-w64 gcc on `PATH`** for CGO — MSYS2 (`pacman -S mingw-w64-x86_64-gcc`) is the usual route. `scripts/win-build.bat` names this requirement in a comment but does not check for it.

### Build

Use the script; it handles the Windows-only steps below that the raw commands do not.

```cmd
scripts\win-build.bat --target=amd64
```

Or by hand:

```cmd
cd app
pnpm install && pnpm run install:electron && pnpm run build
cd ..\kernel
set CGO_ENABLED=1
go build -tags "fts5 sqlcipher" -o "..\app\kernel\SiYuan-Kernel.exe" -ldflags "-s -w" .
```

Installer: `pnpm run dist` in `app/`, producing an NSIS `siyuan-<version>-win.exe` in `app/build`.

### arm64

```cmd
scripts\win-build.bat --target=arm64
```

This needs an **aarch64-w64-mingw32** cross compiler from [llvm-mingw](https://github.com/mstorsjo/llvm-mingw/releases). Put its `bin` directory on `PATH`, or point `SIYUAN_ARM64_CC` at the compiler:

```cmd
set SIYUAN_ARM64_CC=C:\llvm-mingw\bin\aarch64-w64-mingw32-gcc.exe
scripts\win-build.bat --target=arm64
```

The script used to hardcode one developer's install path, so this target built for nobody else; it now resolves the compiler and fails with a clear message if it cannot.

### Windows-only steps the script performs

Worth knowing if you build by hand and get a package that behaves differently from a scripted one:

- **Version resource** — installs and runs `goversioninfo` to embed the icon and version metadata into the exe.
- **`elevator.exe`** — copies `app/elevator/elevator-<arch>.exe` into the kernel directory before packaging.
- **`siyuan.exe` hard link** — created *after* packaging, deliberately, so it is not bundled into the installer. This is what makes `siyuan` work as a CLI command.

CI does none of these three, and CI never builds Windows arm64 or the appx packages at all.

### Unsigned

No code-signing certificate is configured — the `certificateSubjectName` line in `app/electron-builder.yml` is commented out and no workflow supplies `CSC_*` secrets. The installer runs, but SmartScreen will warn about an unknown publisher.

---

## Docker

```bash
docker build -t siyuan-unbound .
```

**Run it from the repository root.** The build context must include `third_party/`, not just `kernel/` — `kernel/go.mod` replaces dejavu with `../third_party/dejavu`, and the Dockerfile stages it as a sibling of `/kernel` for exactly that reason.

Two things differ from every other build:

- **The image omits the `sqlcipher` tag.** `Dockerfile` builds with `-tags fts5` only, so a Docker kernel is not feature-equivalent to a desktop one and encrypted notebooks should not be assumed to work in it.
- **The binary is called `kernel`, not `SiYuan-Kernel`**, because that build passes no `-o`.

See [`DEPLOY.md`](./DEPLOY.md) for running the container, Compose, Unraid and TrueNAS.

---

## Mobile

Android, run from `kernel/`, exactly as CI does:

```bash
gomobile bind -tags "fts5 sqlcipher" -ldflags "-s -w" -v -o kernel.aar -target android/arm64 -androidapi 26 ./mobile/
```

CI pins JDK 21, Android NDK `28.2.13676358`, and installs gomobile at the commit matching `golang.org/x/mobile` in `kernel/go.mod`. Only `android/arm64` is built.

**This produces `kernel.aar` and stops there.** Turning it into an APK needs the separate `siyuan-android` repository plus keystore signing secrets, neither of which is here — so the Android app cannot be built from this repository alone, and the Android job in `cd.yml` will fail on a fork because it pushes to a repository you do not control. The desktop artifacts in that workflow are unaffected.

iOS is documented in `.github/CONTRIBUTING.md` and has no CI job. HarmonyOS is Linux-only and requires patching the Go standard library by hand.

---

## The build paths disagree with each other

Five ways to build the kernel, and they do not produce the same binary. This matters when comparing a local build against a release.

| Source | Command shape | How it differs |
|---|---|---|
| `cd.yml` | `-tags "fts5 sqlcipher"`, `-ldflags "-s -w -X ...util.Mode=prod"` | **the reference build** |
| `scripts/linux-build.sh` | adds `-buildmode=pie`, `-extldflags -static-pie` | no `Mode=prod`, so the binary reports dev mode |
| `scripts/darwin-build.sh` | `-ldflags "-s -w"` | no `Mode=prod` |
| `scripts/win-build.bat` | `-ldflags "-s -w"` | no `Mode=prod` |
| `Dockerfile` | **`-tags fts5` only** | **no `sqlcipher`** — encrypted notebooks |

And around the kernel build:

- CI builds **Windows amd64 only** — never arm64, never appx. Nothing validates those paths.
- `win-build.bat` copies `elevator.exe` and hard-links `siyuan.exe`; CI does neither, so a scripted Windows package and a CI one are assembled from different directory contents.
- The Windows **arm64** electron-builder config bundles no pandoc; the amd64 one does.
- Only CI installs `rpm`. A local Linux build fails that target without it.
- The three local scripts route Go module downloads through `mirrors.aliyun.com` and `goproxy.cn` before falling back to `direct`, and the npm scripts fetch Electron from `npmmirror.com`. Plain `go build` and `pnpm install` do neither.

---

## Verifying without building

These are what CI would catch, and they are safe to run at any time:

```bash
cd app && pnpm run lint                 # tsc typecheck + eslint
cd app && pnpm test                     # node --import tsx --test
cd kernel && gofmt -l . && go vet ./...
cd third_party/dejavu && gofmt -l . && go vet ./... && go test -count=1 ./...
```

`third_party/dejavu` is a **separate Go module** — `go test ./...` in `kernel/` does not run its tests. Its `test/sync` package is a two-client sync simulation and the best regression signal in the repo.

No workflow runs `go test`, `go vet` or `gofmt`. CI builds but never tests, so run these yourself.

---

## Releases through CI

`.github/workflows/cd.yml` has no owner gate, so it runs on a fork. Push a tag matching `*-alpha*`, `*-beta*` or `*-rc*`, or dispatch the workflow manually, and it builds Windows, macOS and Linux installers and attaches them to a Release on your own repository.

Two caveats: the Android job pushes to a repository you do not control and will fail, leaving the desktop artifacts intact; and this path has not been exercised on this fork.

`.github/workflows/dockerimage.yml` is gated on the upstream owner and never runs here — build images yourself.

---

## Troubleshooting

| Symptom | Cause |
|---|---|
| `reading ../third_party/dejavu/go.mod: no such file or directory` | Building from a copy of `kernel/` alone. The vendored sync engine must sit beside it; build from a full clone |
| cgo or linker errors mentioning `gcc`/`clang` | No C compiler. `CGO_ENABLED=1` is required — see your platform's toolchain section |
| `Need executable 'ar' to convert dir to deb` | `binutils` is not installed. `sudo apt-get install binutils` |
| electron-builder fails on the `rpm` target | `rpm` is not installed. `sudo apt-get install rpm`, or take the `tar.gz`/`AppImage` output and ignore the rest |
| macOS: "app is damaged and can't be opened" | Gatekeeper on an unsigned build. `xattr -dr com.apple.quarantine <app>` |
| Windows: SmartScreen "unknown publisher" | Expected — no signing certificate is configured |
| Windows arm64: compiler not found | Install llvm-mingw and set `SIYUAN_ARM64_CC`, or put it on `PATH` |
| Search returns nothing, or encrypted notebooks fail | Built without `fts5` or `sqlcipher`. Both tags are required |

---

## See also

- [`FORK.md`](./FORK.md) — what diverges from upstream, and why
- [`DEPLOY.md`](./DEPLOY.md) — Docker, Compose, Unraid, TrueNAS
- [`../.github/CONTRIBUTING.md`](../.github/CONTRIBUTING.md) — development loop and conventions
