# Sync to storage you own — Reference

This fork lets SiYuan sync through storage you supply — an S3 bucket, a WebDAV server, or a directory on a disk you control — with no SiYuan account and no subscription. This document is the practical guide: how to configure it, what actually lands in your bucket, and what happens when two devices edit the same document.

For why the fork works this way and what else diverges from upstream, see [`FORK.md`](./FORK.md).

---

## 0. In one sentence

Pick a provider in `Settings → Account & Sync`, fill in the connection details, and sync; the kernel writes an encrypted, content-addressed snapshot repository into your storage, and every device that points at the same location converges on the same data.

---

## 1. The four providers

| # | Provider | Needs a SiYuan account? | Suitable for |
|---|---|---|---|
| `0` | SiYuan's official cloud | **Yes — account and active subscription** | Users who would rather not run storage themselves |
| `2` | S3-compatible object storage | No | AWS S3, Cloudflare R2, Backblaze B2, MinIO, Wasabi, and anything else speaking the S3 API |
| `3` | WebDAV | No | Nextcloud, ownCloud, a NAS, any WebDAV server |
| `4` | Local file system | No | A mounted network share, an external disk, a folder another tool already syncs |

Only provider `0` is gated. `SyncProviderRequiresAccount()` in `kernel/model/conf.go` is the single predicate that decides this, and it is true only for provider `0`.

Whichever provider you choose, the data written to it is **encrypted with your data repository key before it leaves the machine**. The storage operator sees opaque chunks. Set the key up first, in `Settings → Account & Sync → Data repo key` — sync will not start without one, and losing it means losing the ability to read your own snapshots.

---

## 2. S3

### Configuration fields

Set in `Settings → Account & Sync`, stored under `sync.s3` in `conf/conf.json` (`kernel/conf/sync.go`).

| Field | Notes |
|---|---|
| `endpoint` | Host of the S3 service, without a scheme prefix — e.g. `s3.amazonaws.com`, `<account>.r2.cloudflarestorage.com`, `192.168.1.10:9000`. Normalised on load |
| `accessKey` / `secretKey` | Credentials. They are stored in `conf.json`; treat that file as a secret |
| `bucket` | Must already exist. SiYuan does not create buckets |
| `region` | Region ID, e.g. `us-east-1`. Providers that ignore regions (R2, MinIO) accept `auto` or any placeholder |
| `pathStyle` | `true` addresses objects as `endpoint/bucket/key`, `false` as `bucket.endpoint/key`. MinIO and most self-hosted gateways need `true`; AWS prefers `false` |
| `skipTlsVerify` | Skips certificate verification. Only for a self-signed endpoint on a network you trust |
| `timeout` | Seconds per request. Clamped to 7–300; `0` becomes 60 |
| `concurrentReqs` | Parallel requests. Clamped to 1–16, default 8 |

If the endpoint is behind a reverse proxy that rewrites the `Host` header, SigV4 signing will fail with a signature mismatch. Point the endpoint at the origin, or configure the proxy to preserve `Host`.

### What lands in your bucket

The sync directory name (`Settings → Account & Sync → Cloud sync directory`, `sync.cloudName` in `conf.json`) namespaces the objects, so one bucket can hold several independent workspaces:

```
<bucket>/
├── repo/                          ← the sync directory named "main"
│   ├── indexes/<id>
│   ├── objects/<2-char>/<hash>
│   └── refs/latest
└── siyuan/
    ├── work/repo/...                ← the sync directory named "work"
    └── archive/repo/...             ← the sync directory named "archive"
```

`main` maps to the bucket root rather than to `siyuan/main/`. That is not an inconsistency — it is what upstream wrote, and keeping it means existing buckets continue to work with no migration. Every other name is namespaced under `siyuan/<name>/`.

Directory names are validated: 1–63 characters, no whitespace and no punctuation beyond `-` and `_`. `IsValidCloudDirName` in `third_party/dejavu/cloud/cloud.go` is the authority, and both `CreateRepo` and `RemoveRepo` re-check it before touching a key.

### Existing S3 users: a one-shot repair

Before this change, listing S3 sync directories returned the **bucket name**, and the client wrote that name back into `sync.cloudName`. With key prefixing in place, a leftover bucket name would resolve to `siyuan/<bucket>/repo/`, the cloud would read as empty, and the whole workspace would be re-uploaded — orphaning the existing data.

So on the first startup after upgrading, if the provider is S3 and `cloudName` is not `main`, it is reset to `main` and the reset is logged to `<workspace>/temp/siyuan.log`. A `s3CloudNameMigrated` flag in `conf.json` makes this happen exactly once. New installations have the flag set from the start, so it never fires for them.

If you genuinely use a non-`main` sync directory on S3 and had it configured before upgrading, set it again after the first startup.

---

## 3. WebDAV

| Field | Notes |
|---|---|
| `endpoint` | Full URL of the WebDAV collection, e.g. `https://cloud.example.com/remote.php/dav/files/alice/siyuan/` |
| `username` / `password` | For Nextcloud and ownCloud, prefer an app password over the account password |
| `skipTlsVerify` | As for S3 |
| `timeout` | Seconds per request, clamped to 7–300 |
| `concurrentReqs` | Clamped to 1–16, default 1 |

The default of one concurrent request is deliberate: many WebDAV servers serialise writes or rate-limit aggressively, and raising it is the usual cause of sync failures against Nextcloud. Raise it only if your server is known to cope.

## 4. Local file system

| Field | Notes |
|---|---|
| `endpoint` | An absolute path to a directory, e.g. `/mnt/nas/siyuan` or `D:\Sync\SiYuan` |
| `timeout` | Seconds, clamped to 7–300 |
| `concurrentReqs` | Clamped to 1–1024, default 16 |

Useful with a mounted network share, or with a directory that Syncthing or a similar tool already replicates. It is genuinely different from pointing a folder-sync tool at your workspace: the kernel writes an immutable, content-addressed repository here, so a file-level sync tool replicating *this* directory cannot corrupt the workspace the way replicating the workspace itself can.

The path must be reachable when sync runs. A disconnected network mount produces sync errors rather than silent data loss.

---

## 5. What happens when two devices edit the same document

### The ordinary case

Different documents on each device, or different blocks of the same document: both sides' edits are kept, automatically, with no conflict document.

The kernel performs a three-way merge at the block level. `.sy` documents are block trees where every block has a stable ID, and a sync already has all three versions it needs — the last synced index is the common ancestor. Blocks that only the cloud changed are spliced onto the **local** tree; everything else keeps the local version.

### When the merge refuses

The merge is narrow by design. It hands the document back to upstream's conflict handling whenever it cannot be certain, which includes:

- **The same block was edited on both sides.** There is no correct automatic answer, so the whole document becomes a conflict.
- **The block structure differs.** Blocks added, removed, moved, or reordered on either side. The merge only substitutes content into an identical structure.
- **The changed block contains other blocks** — a list, a blockquote, a superblock. Splicing a container would carry its children along.
- **Duplicate or empty block IDs.** Such a document cannot be addressed by ID, so a substitution could land in the wrong block.
- **Encrypted notebooks.** Their content is ciphertext at this layer, so there is nothing to compare; they always fall through to a conflict.
- **The file is not a `.sy` document** — assets and attribute-view data are opaque here.

This asymmetry is intentional. The local file is the base and is never overwritten, so a missed detection can only fail to apply a *cloud* edit, which the next sync offers again. It can never discard a local one.

### What a conflict document is

When the merge declines, upstream behaviour applies: **your local file is left exactly as it is**, and the other side's version is written alongside it as a separate document with `Conflicted` in the title (`Conf.Sync.GenerateConflictDoc`, on by default in this fork). Both versions are also captured in `history/<timestamp>-sync/`, browsable through the history browser (`Data History → Data repo snapshots`).

Nothing is discarded. Reconciling the two is a manual edit, which is the point — the machine got out of its depth and said so.

---

## 6. Devices and the sync lock

Each installation has a device identifier, used to coordinate a lock so two devices do not write to the cloud repository at once.

That identifier is stored in `device.id` in the user config directory (`~/.config/siyuan/`), **not** in the workspace — see [`WORKSPACE.md`](./WORKSPACE.md#outside-the-workspace-the-user-config-directory). Copying a workspace folder to another machine therefore does not transplant the device identity: the second machine uses its own, or generates one on first run. This matters, because two devices sharing an identifier would each treat the other's lock as their own and could write concurrently.

Upgrading an existing installation regenerates the identifier once. Nothing has to be cleaned up: the cloud lock records the holder's identifier and is treated as expired after 65 seconds, so a lock left behind under the old identifier clears itself.

---

## 7. Troubleshooting

| Symptom | Likely cause |
|---|---|
| Sync will not start at all | No data repository key. `Settings → Account & Sync → Data repo key` |
| S3 signature mismatch | A reverse proxy rewriting `Host`, or `pathStyle` set the wrong way for your provider |
| The whole workspace re-uploads after upgrading | The one-shot S3 `cloudName` repair (§2) did not apply, or a pre-upgrade `conf.json` was restored over it. Check `<workspace>/temp/siyuan.log` for the reset line |
| Frequent WebDAV failures | `concurrentReqs` raised above what the server tolerates. Try `1` |
| Conflict documents appearing constantly | Both devices are editing the same blocks, or the document's structure changes on both sides — headings folded and unfolded do **not** count as edits, but moving blocks does |
| Sync reports the repository is locked by another device | Another device is mid-sync, or one stopped abruptly. The lock expires after 65 seconds; if it persists longer, a second instance is running against the same storage |

Sync activity is logged to `<workspace>/temp/siyuan.log`. Each completed sync records the device, provider, mode, transferred bytes, and the counts of conflicts, upserts and removes.

---

## 8. What still requires a subscription

Unchanged from upstream, and deliberately so — these run on SiYuan's own infrastructure, not on yours:

- Official cloud sync (provider `0`)
- Cloud image and asset hosting
- Cloud reminders
- CDN asset rewriting on export
- Liandi publishing and account services

Sync to your own S3, WebDAV or local storage requires none of them, and no account at all.
