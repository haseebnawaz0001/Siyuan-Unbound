# Deploying with Docker — Reference

This fork publishes no Docker image. `.github/workflows/dockerimage.yml` is gated `if: ${{ github.repository_owner == 'siyuan-note' }}`, so the publish job never runs on a fork — there is no `haseebnawaz0001/siyuan-unbound` (or similar) image sitting on Docker Hub waiting for you. Every deployment path below starts from building the image yourself with the repository's own `Dockerfile`.

For how the build differs from the other build paths in this repo (and why that matters for encrypted notebooks), see [`BUILD.md`](./BUILD.md).

---

## 1. Building the image

From the repository root:

```bash
docker build -t siyuan-unbound .
```

Run this from the repo root, not from `kernel/`. `Dockerfile` stages `third_party/` as a sibling of `/kernel` inside the build container before the Go module graph is resolved — `kernel/go.mod` replaces `github.com/siyuan-note/dejavu` with `../third_party/dejavu`, and `go mod download` fails on a missing replacement directory if the build context does not include `third_party/` alongside `kernel/`. The relevant comment lives at `Dockerfile:37-39`.

The resulting image is a three-stage build: a Node stage that builds `app/` (`appearance`, `stage`, `guide`, `changelogs`), a Go stage that builds the kernel with `go build -tags fts5 -ldflags "-s -w"`, and a slim `alpine:latest` runtime stage that copies both artifacts in, with `su-exec` and `tzdata` installed for the entrypoint. The entrypoint is `/opt/siyuan/entrypoint.sh`, and the default command is `["/opt/siyuan/kernel", "serve"]`.

Substitute whatever tag you like for `siyuan-unbound` — it is used as-is for the rest of this document.

---

## 2. Running it

### Entrypoint

The entry point is set when building the image: `ENTRYPOINT ["/opt/siyuan/entrypoint.sh"]`. This script (`kernel/entrypoint.sh`) creates or reuses a `siyuan` user/group at the given `PUID`/`PGID`, `chown -R`s `/opt/siyuan`, `/home/siyuan/` and the workspace directory to that UID/GID, then execs the kernel via `su-exec`. This is especially relevant to solve permission issues when mounting directories from the host.

Use the following parameters when running the container with `docker run siyuan-unbound`:

> **Note:** the `serve` subcommand must be passed explicitly (e.g. `docker run siyuan-unbound serve --workspace=...`). Run `docker run --rm siyuan-unbound serve --help` to see all serving options.

- `--workspace`: Specifies the workspace folder path, mounted to the container via `-v` on the host
- `--accessAuthCode`: Specifies the lock screen password

More parameters can be found using `--help`. Here's an example startup command:

```bash
docker run -d \
  -v workspace_dir_host:workspace_dir_container \
  -p 6806:6806 \
  -e PUID=1001 -e PGID=1002 \
  siyuan-unbound \
  serve \
  --workspace=workspace_dir_container \
  --accessAuthCode=xxx
```

- `PUID`: Custom user ID (optional, defaults to `1000` if not provided)
- `PGID`: Custom group ID (optional, defaults to `1000` if not provided)
- `workspace_dir_host`: The workspace folder path on the host
- `workspace_dir_container`: The path of the workspace folder in the container, as specified in `--workspace`
  - Alternatively, it's possible to set the path via the `SIYUAN_WORKSPACE_PATH` env variable. The commandline will always have the priority, if both are set
- `accessAuthCode`: Lock screen password (please **be sure to modify**, otherwise anyone can access your data)
  - Alternatively, it's possible to set the lock screen password via the `SIYUAN_ACCESS_AUTH_CODE` env variable. The commandline will always have the priority, if both are set
  - To disable the lock screen password set the env variable `SIYUAN_ACCESS_AUTH_CODE_BYPASS=true`
- `SIYUAN_LANG`: Interface language (optional, defaults to `en` if unset in Docker). Accepts BCP 47 tags like `zh-CN`/`zh-TW`/`en`/`ja`/`pt-BR`; legacy underscore values like `zh_CN`/`en_US` are also accepted for backward compatibility. Omit it if you want the language chosen in **Settings** to persist across restarts; if set, it is applied on every startup and overrides the saved setting
  - Alternatively, use the `--lang` command-line parameter. If both are set, the command-line takes priority

To simplify things, it is recommended to configure the workspace folder path to be consistent on the host and container, such as having both `workspace_dir_host` and `workspace_dir_container` configured as `/siyuan/workspace`. The corresponding startup command would be:

```bash
docker run -d \
  -v /siyuan/workspace:/siyuan/workspace \
  -p 6806:6806 \
  -e PUID=1001 -e PGID=1002 \
  siyuan-unbound \
  serve \
  --workspace=/siyuan/workspace/ \
  --accessAuthCode=xxx
```

### User Permissions

The `entrypoint.sh` script ensures the creation of the `siyuan` user and group with the specified `PUID` and `PGID`. Therefore, when the host creates a workspace folder, pay attention to setting the user and group ownership of the folder to match the `PUID` and `PGID` you plan to use. For example:

```bash
chown -R 1001:1002 /siyuan/workspace
```

If you use custom `PUID` and `PGID` values, the entrypoint script will ensure that the correct user and group are created inside the container, and ownership of mounted volumes will be adjusted accordingly. There's no need to manually pass `-u` in `docker run` or `docker-compose` as the environment variables will handle the customization.

### Hidden port

Use an NGINX reverse proxy to hide port 6806. Please note:

- Configure the WebSocket reverse proxy for `/ws`

### Note

- Be sure to confirm the correctness of the mounted volume, otherwise the data will be lost after the container is deleted
- Do not use URL rewriting for redirection, otherwise there may be problems with authentication, it is recommended to configure a reverse proxy
- If you encounter permission issues, verify that the `PUID` and `PGID` environment variables match the ownership of the mounted directories on your host system

---

## 3. Docker Compose

There is no registry image to pull, so `image:` becomes a `build:` stanza pointing at the repo root (or a tag you built manually with the `docker build` command from section 1 and reference here).

```yaml
version: "3.9"
services:
  main:
    build: .
    command: ['serve', '--workspace=/siyuan/workspace/', '--accessAuthCode=${AuthCode}']
    ports:
      - 6806:6806
    volumes:
      - /siyuan/workspace:/siyuan/workspace
    restart: unless-stopped
    environment:
      - TZ=${YOUR_TIME_ZONE}    # A list of time zone identifiers can be found at https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
      - PUID=${YOUR_USER_PUID}  # Customize user ID
      - PGID=${YOUR_USER_PGID}  # Customize group ID
```

`build: .` assumes the compose file lives at the repository root, matching the build-context requirement from section 1. If your compose file lives elsewhere, point `build:` at the repo root path instead (e.g. `build: /path/to/siyuan`).

In this setup:

- `PUID` and `PGID` are set dynamically and passed to the container
- If these variables are not provided, the default `1000` will be used

By specifying `PUID` and `PGID` in the environment, you avoid the need to explicitly set the `user` directive (`user: '1000:1000'`) in the compose file. The container will dynamically adjust the user and group based on these environment variables at startup.

---

## 4. Unraid and TrueNAS

Both platforms' app templates normally expect a published registry image. Since this fork publishes none, you must first build the image (section 1) and push it to a registry you control — Docker Hub, GHCR, or a local registry — before either template below will pull anything.

```bash
docker build -t ghcr.io/<you>/siyuan-unbound:latest .
docker push ghcr.io/<you>/siyuan-unbound:latest
```

Substitute your own registry namespace and tag; the template fields below assume the pushed reference resolves for the Unraid or TrueNAS host.

### Unraid

Note: First run `chown -R 1000:1000 /mnt/user/appdata/siyuan` in the terminal

Template reference:

```
Web UI: 6806
Container Port: 6806
Container Path: /home/siyuan
Host path: /mnt/user/appdata/siyuan
PUID: 1000
PGID: 1000
Publish parameters: serve --accessAuthCode=******(Lock screen password)
```

The template's image field must point at the reference you pushed above (e.g. `ghcr.io/<you>/siyuan-unbound:latest`), not `b3log/siyuan`.

### TrueNAS

Note: First, run the commands below in the TrueNAS Shell. Please update `Pool_1/Apps_Data/siyuan` to match your dataset path.

```shell
zfs create Pool_1/Apps_Data/siyuan
chown -R 1001:1002 /mnt/Pool_1/Apps_Data/siyuan
chmod 755 /mnt/Pool_1/Apps_Data/siyuan
```

Navigate to Apps - DiscoverApps - More Options (on top right, besides Custom App) - Install via YAML

Template reference:

```yaml
services:
  siyuan:
    image: ghcr.io/<you>/siyuan-unbound:latest
    container_name: siyuan
    command: ['serve', '--workspace=/siyuan/workspace/', '--accessAuthCode=2222']
    ports:
      - 6806:6806
    volumes:
      - /mnt/Pool_1/Apps_Data/siyuan:/siyuan/workspace  # Adjust to your dataset path
    restart: unless-stopped
    environment:
      - TZ=America/New_York  # Replace with your timezone if needed
      - PUID=1001
      - PGID=1002
```

`image:` has to be the reference you pushed to your own registry — TrueNAS has no way to resolve `b3log/siyuan` into anything this fork ships, and this fork ships nothing at that name regardless.

---

## 5. Limitations

- Does not support desktop and mobile application connections, only supports use on browsers
- Export to PDF, HTML and Word formats is not supported
- Import Markdown file is not supported

---

## 6. Caveats specific to this fork

**No automated publishing.** `.github/workflows/dockerimage.yml` is gated `if: ${{ github.repository_owner == 'siyuan-note' }}` and therefore never runs on a fork, regardless of tags pushed or workflow dispatches triggered. Building locally per section 1 is the only path to an image.

**The Docker build omits the `sqlcipher` build tag.** `Dockerfile:46` builds the kernel with `go build -tags fts5 -ldflags "-s -w"`, while every other build path in this repo — `.github/workflows/cd.yml:198` and all three of `scripts/darwin-build.sh`, `scripts/win-build.bat`, `scripts/linux-build.sh` — builds with `-tags "fts5 sqlcipher"`. SQLCipher backs encrypted notebooks: `kernel/sql/database.go` and `kernel/treenode/blocktree.go` both derive SQLCipher keys (`DeriveSubKey(dek, "siyuan/sqlcipher/content")` and `DeriveSubKey(dek, "siyuan/sqlcipher/blocktree")` respectively) to open the per-notebook content and block-tree databases. A kernel built from this `Dockerfile` is therefore not feature-equivalent to a release binary, and encrypted notebooks should not be assumed to work in a Docker deployment. This predates the fork and is documented here rather than fixed; see [`BUILD.md`](./BUILD.md) for the full build-path comparison.
