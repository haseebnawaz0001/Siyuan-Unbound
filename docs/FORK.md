# How this fork differs from upstream SiYuan

This repository is a fork of [SiYuan](https://github.com/siyuan-note/siyuan). It is not a rewrite — the overwhelming majority of the code is upstream's, and the intent is to stay close enough to keep merging from it. This document records every deliberate divergence, so that a future rebase can tell an intentional change from an accident.

Read it alongside [`SYNC.md`](./SYNC.md), which explains how to actually use the sync changes described here, and [`AGENTS.md`](../AGENTS.md), which encodes the rules an agent working in this repo has to follow.

Licence is unchanged: AGPL-3.0, as upstream. Running a modified copy for yourself is exactly what that licence is for.

---

## 1. Telemetry removed

**Upstream:** derives a device identifier from the host hardware via `denisbrodbeck/machineid`, pulls an announcement feed from the cloud on startup, reports through several paths in `kernel/model/cloud_service.go`, and checks for new versions — on a six-hourly timer, ten seconds after the user guide opens, and from a button in Settings. On Windows and macOS it downloads the installer unprompted, since that setting defaults to on.

**This fork:** `util.GetDeviceID()` returns a random string generated once per installation, and the hardware fingerprint dependency is gone from `kernel/go.mod`. The announcement pull and the `cloud_service.go` reporting paths were deleted. The whole update checker went with them — `kernel/model/updater.go`, both six-hourly cron entries, the user-guide trigger, `/api/system/checkUpdate`, `/api/system/setDownloadInstallPkg` and the `downloadInstallPkg` setting. There is no release channel here; you get a new version by pulling and rebuilding, which is what `docs/BUILD.md` describes.

**What was kept, and what that costs:** the `rhy` endpoint — `GET <cloud>/apis/siyuan/version?ver=<version>` — survives because the marketplace resolves its index through it (`kernel/bazaar/stage.go` → `util.GetRhyBazaarHash` → `util.GetRhyResult`). Removing it would break browsing and installing plugins, themes, icons and templates.

It is now reached **only on demand.** `GetRhyBazaarHash` fetches lazily when its cache is empty, and with the cron warmer gone it is the sole path to the endpoint — verifiably so: `GetRhyResult` has exactly one caller in the whole kernel, inside `GetRhyBazaarHash`, which in turn is called only from `kernel/bazaar/stage.go`. Nothing on boot, nothing on a timer, nothing when the user guide opens. The endpoint is contacted the first time you open the marketplace in a session, and not otherwise.

Two things still contact SiYuan's servers, but only once you have asked them to: sync, if you have configured the official cloud provider, and the account refresh, which runs two-hourly but returns immediately unless you are signed in. Sync to your own storage and an unauthenticated install trigger neither.

Commits `91af2265e`, and the update-checker removal that followed.

## 2. English is the primary language

**Upstream:** detects the OS locale on startup — in `kernel/model/conf.go` and again in `app/electron/main.js` — and picks the UI language and cloud region from it. Source comments are written in Chinese.

**This fork:** the language is `en` and the cloud region is North America (`cloudRegion: 1`) unless the user changes them. The language switcher is untouched and every translation still ships; English is the default, not a lock. Roughly 8,700 lines of source comment across the Go kernel, the TypeScript frontend, the SCSS and the build scripts were rewritten in English by meaning rather than translated word for word.

**What is deliberately still not English:** content where the Chinese text is functional rather than prose — Han-equivalence search fixtures, Chinese slash-command aliases, strings matched against output from the OS or from LLM providers, and of course the i18n language files themselves. `AGENTS.md` §6.4 states the rule.

Commit `b5fea5fdb`.

## 3. Sync works without a subscription — for storage you supply

This is the change the fork exists for, and it is also the one with a line drawn through the middle of it.

**Upstream:** every sync provider is gated behind `IsSubscriber()`. S3, WebDAV and local-filesystem sync are fully implemented but unreachable without an active SiYuan subscription.

**This fork:** the gate is now `SyncProviderRequiresAccount(provider) && !IsSubscriber()`, and `SyncProviderRequiresAccount` (`kernel/model/conf.go`) is true only for provider `0`, SiYuan's own cloud. Providers `2` (S3), `3` (WebDAV) and `4` (local filesystem) talk exclusively to storage the user supplies, so they need no account and no subscription. The matching frontend gates were removed from `app/src/config/tabs/syncUi.ts`, `app/src/sync/syncGuide.ts`, `app/src/protyle/wysiwyg/transaction.ts`, `app/src/card/openCard.ts` and the onboarding flow.

**What is still gated, and why.** Everything that runs on SiYuan's own infrastructure. Not as an oversight — deliberately:

| Feature | Where |
|---|---|
| Official cloud sync (provider `0`), including the sync WebSocket | `kernel/model/sync.go`, `kernel/model/repository.go` |
| Cloud image / asset hosting | `kernel/model/assets.go` |
| Cloud reminders | `kernel/model/blockial.go` |
| CDN asset rewriting on export | `kernel/model/export.go` |
| Liandi publishing and account services | `kernel/model/cloud_service.go` |

AGPL-3.0 grants the right to modify and run your own copy. It does not grant free use of somebody else's servers. Unlocking a client-side check on a feature that only ever touches your own bucket is the former; unlocking one that consumes SiYuan's hosting is the latter. Those gates stay.

Commits `1aa278de1`, `30f0bd636`.

### 3a. S3 object keys are namespaced by sync directory

Upstream's S3 provider ignored the configured sync directory and wrote every object to the bucket root, so one bucket held exactly one workspace. Keys are now prefixed with `siyuan/<dir>/repo/...`, matching what the WebDAV and local providers already did. The directory named `main` keeps the bucket-root layout, so existing buckets keep working untouched. `CreateRepo` and `RemoveRepo` are implemented, and `listRepos` enumerates key prefixes instead of listing buckets.

Existing S3 users get a one-shot config repair on first startup — see [`SYNC.md`](./SYNC.md#existing-s3-users-a-one-shot-repair).

### 3b. Conflicting documents merge block by block

Upstream resolves a document that changed on both sides by keeping the local file and writing the cloud version out as a separate conflict document. That is safe but blunt: two devices editing different paragraphs of the same document produce a conflict every time.

This fork adds a three-way, block-level merge. `.sy` documents are block trees with stable IDs, and a sync already has all three versions it needs — the last synced index is the common ancestor. Blocks only the cloud changed are spliced onto the local tree; everything else keeps the local version. The merge is deliberately narrow and refuses in every ambiguous case, falling back to upstream's conflict document. Details and the exact refusal conditions are in [`SYNC.md`](./SYNC.md#5-what-happens-when-two-devices-edit-the-same-document).

The direction matters and is load-bearing: the local file is the base and is never overwritten, so a bug in the change detector can only fail to *apply* a cloud edit — which the next sync offers again — and can never discard a local one.

## 4. The dejavu sync engine is vendored, not depended on

**Upstream:** `github.com/siyuan-note/dejavu` is an ordinary Go module dependency.

**This fork:** it lives in-repo at [`third_party/dejavu`](../third_party/dejavu), forked from `siyuan-note/dejavu@v0.0.0-20260715095305-8462fe30163c`, and `kernel/go.mod` carries a committed `replace` directive pointing at it. The block merge in §3b could not be implemented any other way — it belongs inside the sync engine, not around it.

This is the one place where the fork breaks a rule that otherwise still holds. `AGENTS.md` §2 says never to commit a `replace` directive, and that remains correct for every other dependency; the local-checkout `replace` lines commented out at the bottom of `kernel/go.mod` are exactly what it warns about. The dejavu one is different because it points at a path inside the repository, so it resolves for every clone and in CI rather than only on one developer's machine.

Two consequences worth knowing:

- **`third_party/dejavu` is a separate Go module.** `go test ./...` in `kernel/` does not run its tests. Verify changes from inside that directory.
- **Anything that stages only `kernel/` will fail.** The root `Dockerfile` had to be taught to copy `third_party/` into the build image as a sibling of `/kernel`, or `go mod download` cannot resolve the replacement.

Beyond the merge, the fork changes S3 key prefixing and repository create/remove (§3a), and fixes an upstream data race in `getNotFound` where concurrent workers appended to a shared slice — a lost append reports a chunk missing from the cloud as present, so it is never uploaded and the cloud repository is silently left incomplete.

## 5. The device identifier lives outside the workspace

**Upstream:** derives the identifier from the hardware, so it is inherently per-machine.

**This fork:** with the fingerprint removed (§1), the identifier had to be anchored somewhere. Storing it only in `conf.json` would put it inside the workspace, and copying a workspace folder to a second machine would carry the identity along — the cloud sync lock treats a lock held by its own device ID as free to take, so two machines sharing an identifier could each see the other's lock as their own and write concurrently.

It is therefore recorded in `device.id` in the user config directory (`~/.config/siyuan/`), next to the existing `port.json` and `cookie.key`, and read from there at startup. `conf.json`'s `system.id` is now a cache of that value, used only if the file cannot be written. See [`WORKSPACE.md`](./WORKSPACE.md#outside-the-workspace-the-user-config-directory).

Existing installations regenerate their identifier once, on the first startup after this change. Nothing needs cleaning up afterwards: a cloud lock is treated as expired 65 seconds after it was taken, so one left behind under the old identifier clears itself.

Commit `740cb8094`.

---

## Rebasing from upstream: where conflicts will land

- **Comment language.** Upstream writes comments in Chinese and this fork writes them in English, so essentially every touched file conflicts on comments. This is the largest recurring cost of the fork and it was accepted knowingly.
- **`kernel/go.mod`.** The dejavu `replace` must survive. Upstream bumping the dejavu version changes nothing here — the `replace` wins — which also means **upstream fixes to dejavu do not reach this fork automatically**. They have to be merged into `third_party/dejavu` by hand.
- **`IsSubscriber()` call sites.** Only the sync ones were changed. If upstream adds a gate, the default should be to leave it alone; only a gate on a feature that touches nothing but the user's own storage belongs on the other side of the line drawn in §3.
- **`kernel/model/conf.go` startup path.** Locale detection (§2), the device identifier (§5) and the one-shot S3 config repair (§3a) all live in `InitConf`, close together, in code upstream also edits.
- **`third_party/dejavu/sync.go`.** The conflict branch and `MergeResult` diverge from upstream. `sync_merge_blocks.go` and its tests are new files and will not conflict.
