<p align="center">
<img alt="SiYuan" src="app/stage/icon.png" width="128">
<br><br>
<a href="https://www.gnu.org/licenses/agpl-3.0.txt"><img src="https://img.shields.io/badge/license-AGPLv3-blue.svg" alt="License: AGPL v3"></a>
</p>

<p align="center">
<a href="README.md">English</a> | <strong>中文</strong> | <a href="README.ja.md">日本語</a> | <a href="README.tr.md">Türkçe</a>
</p>

---

## 简介

**SiYuan Unbound 是 [思源笔记](https://github.com/siyuan-note/siyuan) 的一个 fork**，思源是一款隐私优先的个人知识管理系统，支持块级引用和 Markdown 所见即所得。这不是一次重写——绝大部分代码仍然是上游的，这个 fork 也刻意保持与上游足够接近，以便持续合并上游的更新。

这个 fork 与上游的差异只有四点：

- **同步不再需要订阅，只需要你自己的存储。** S3 兼容对象存储、WebDAV 和本地文件系统目录都可以直接同步，不需要任何账号。思源官方的云端服务仍然是收费的——见下文。
- **移除了遥测。** 没有硬件设备指纹采集，也没有自动拉取公告。
- **默认语言是英文**，源码注释也是英文。语言切换功能照常可用，所有语言包也都还在。
- **文档冲突时按块合并**，而不是永远由一方覆盖另一方。

[`docs/FORK.md`](docs/FORK.md) 记录了每一处差异、为什么要改，以及将来对上游做 rebase 时会在哪里冲突。

这是一个非官方 fork，不受思源笔记项目支持、认可，也与该项目没有任何关联，没有应用商店上架，没有发布 Docker 镜像，也没有支持渠道。许可证不变，仍是 AGPL-3.0。

![Editing and block references](screenshots/feature0.png)

![Database views](screenshots/feature5-1.png)

## 特性

- 内容块
  - 块级引用和双向链接
  - 自定义属性
  - SQL 查询嵌入
  - 协议 `siyuan://`
- 编辑器
  - Block 风格
  - Markdown 所见即所得
  - 列表大纲
  - 块缩放聚焦
  - 百万字大文档编辑
  - 数学公式、图表、流程图、甘特图、时序图、五线谱等
  - 网页剪藏
  - PDF 标注双链
- 导出
  - 块引用和嵌入块
  - 带 assets 文件夹的标准 Markdown
  - PDF、Word 和 HTML
  - 复制到微信公众号、知乎和语雀
- 数据库
  - 表格视图
- 闪卡间隔重复
- 接入 OpenAI 接口支持人工智能写作和问答聊天
- Tesseract OCR
- 多页签，可拖拽分屏
- 模板片段
- JavaScript/CSS 代码片段
- 同步到自己的 S3 / WebDAV / 本地存储，无需账号
- Docker 部署
- 社区集市

### 哪些仍然要付费

凡是跑在思源笔记自己服务器上的东西，都被有意保留为收费项：官方云端同步、云端图片和资源文件托管、云端提醒、导出时的 CDN 资源转换，以及链滴发布。AGPL-3.0 允许你修改和运行自己的副本，但不代表可以免费白嫖别人的基础设施。如果需要这些服务，参见[定价](https://b3log.org/siyuan/pricing.html)。

## 获取构建产物

这里没有下载包，只能自己构建。

```bash
git clone git@github.com:haseebnawaz0001/Siyuan-Unbound.git
cd Siyuan-Unbound

cd app && pnpm install && pnpm run install:electron && pnpm run build && cd ..

cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..

./app/kernel/SiYuan-Kernel serve
```

你需要 Go 1.26、Node 24、pnpm 11.12.0 和一个 C 编译器。内核通过 HTTP 提供前端服务，所以最后这条命令就是一次完整可用的安装——不需要安装程序。

关于桌面端安装包、交叉编译、Docker、移动端，以及本仓库里五条构建路径之间彼此不一致的地方，请阅读 **[`docs/BUILD.md`](docs/BUILD.md)**。

### 或者交给 CI 构建

`.github/workflows/cd.yml` 没有设置 owner 限制，所以在 fork 上也能跑。推送一个匹配 `*-alpha*`、`*-beta*` 或 `*-rc*` 的 tag，或者手动触发该 workflow，它就会构建 Windows、macOS 和 Linux 安装包，并附加到你自己仓库的一个 Release 上。

有两点需要注意：这个 workflow 里的 Android 任务会推送到一个你不掌控的仓库，因此会失败——但不影响桌面端产物的构建；另外这条路径在这个 fork 上还没有被实际验证过。

## 自托管

构建镜像并运行：

```bash
docker build -t siyuan-unbound .
```

这个 fork 不发布也无法发布镜像：`.github/workflows/dockerimage.yml` 的发布任务被限定为只能由上游 owner 触发，所以在这里永远不会运行。**[`docs/DEPLOY.md`](docs/DEPLOY.md)** 介绍了运行容器、Docker Compose、Unraid 和 TrueNAS 的方法，以及一个重要的注意事项——Docker 构建省略了 `sqlcipher` 编译标签，所以不应假定加密笔记本在其中可用。

## 文档

| 文档 | 解答的问题 |
|---|---|
| [FORK.md](docs/FORK.md) | 这个 fork 与上游有哪些差异，以及为什么这么改 |
| [SYNC.md](docs/SYNC.md) | 如何针对自己的 S3、WebDAV 或本地存储配置同步 |
| [BUILD.md](docs/BUILD.md) | 各平台从源码构建的方法 |
| [DEPLOY.md](docs/DEPLOY.md) | Docker、Compose、Unraid、TrueNAS |
| [WORKSPACE.md](docs/WORKSPACE.md) | 工作空间在磁盘上是什么样子 |
| [SY-FORMAT.md](docs/SY-FORMAT.md) | `.sy` 文档格式 |
| [ENCRYPTED-NOTEBOOK.md](docs/ENCRYPTED-NOTEBOOK.md) | 加密笔记本的工作原理 |
| [API.md](docs/API.md) | 内核 HTTP API |
| [CONTRIBUTING.md](.github/CONTRIBUTING.md) | 开发环境搭建和约定 |
| [AGENTS.md](AGENTS.md) | 仓库指南，包括编码 agent 在这里必须遵守的规则 |

## 命令行接口

内核可执行文件本身就是 CLI，它直接读取工作空间数据——不需要运行服务器。

```bash
# 列出所有笔记本
siyuan notebook list -w ~/SiYuan

# 全文搜索，JSON 输出
siyuan search "keyword" -w ~/SiYuan -f json

# 搜索资源文件内容（PDF/Word/Excel/txt 等）
siyuan search "phrase" --asset -w ~/SiYuan
siyuan search "phrase" --asset --ext pdf --ext docx -w ~/SiYuan

# 将文档导出为 Markdown
siyuan export md --id <block-id> -w ~/SiYuan
```

| 分类 | 命令 |
|----------|----------|
| 笔记本与文档 | `notebook`、`document`、`dailynote` —— 增删改查和每日笔记 |
| 内容 | `block`、`attr`、`outline` —— 块读写、属性、大纲 |
| 元数据 | `tag`、`bookmark`、`template` —— 标签、书签、模板片段 |
| 查询 | `search`、`sql` —— 全文、语义、资源文件内容以及 SQL 查询 |
| 引用 | `ref` —— 反向链接和提及 |
| 导入导出 | `export`、`import`、`inbox` —— Markdown、HTML、preview、Word、.sy.zip、Data、云端收集箱 |
| 数据管理 | `repo`、`history`、`sync` —— 快照、版本、云端同步 |
| 工具 | `asset`、`file` —— 资源与文件系统 |
| 数据库 | `database` —— 属性视图管理 |
| 服务 | `serve` —— 启动内核 HTTP 服务器 |
| 工作空间与系统 | `workspace`、`system` —— 列出、查看、系统信息 |

运行 `siyuan --help` 查看完整命令树。使用 `-f json`（默认是 `-f table`）获得适合脚本处理的输出。大多数写操作命令还支持 `--dry-run`，可以在不实际执行的情况下预览将要发生的改动。

可执行文件位于 `<install-dir>/resources/kernel/SiYuan-Kernel`，或者你自己构建时的输出路径。想以 `siyuan` 命令调用它，把它软链接到 `PATH` 里：

```bash
ln -s <install-dir>/resources/kernel/SiYuan-Kernel /usr/local/bin/siyuan
```

## 架构和生态

| Project | Role |
|---|---|
| [lute](https://github.com/88250/lute) | 编辑器引擎——Markdown/`.sy` 的 AST |
| [dejavu](third_party/dejavu) | 数据仓库和同步引擎——**内置分支**，见 [FORK.md](docs/FORK.md) 第 4 节 |
| [riff](https://github.com/siyuan-note/riff) | 间隔重复调度器 |
| [bazaar](https://github.com/siyuan-note/bazaar) | 社区集市 |
| [petal](https://github.com/siyuan-note/petal) | 插件 API |
| [chrome](https://github.com/siyuan-note/siyuan-chrome) | 网页剪藏扩展 |
| [android](https://github.com/siyuan-note/siyuan-android) / [ios](https://github.com/siyuan-note/siyuan-ios) / [harmony](https://github.com/siyuan-note/siyuan-harmony) | 封装 gomobile 内核的移动端 App |

除了 `dejavu`，其余都是上游维护、这个 fork 原样使用的项目。移动端 App 和剪藏扩展都是通过 HTTP API 与内核通信的，所以它们可以对接这个 fork，但构建它们不在本仓库的范围内——见 [`docs/BUILD.md`](docs/BUILD.md) 第 6 节。

## 常见问题

### 思源是如何存储数据的？

数据保存在工作空间的 data 文件夹下：

- `assets` 用于保存所有插入的资源文件
- `emojis` 用于保存表情图片
- `snippets` 用于保存代码片段
- `storage` 用于保存查询条件、布局和闪卡数据等
- `templates` 用于保存模板片段
- `widgets` 用于保存挂件
- `plugins` 用于保存插件
- `public` 用于保存公开数据
- 其余文件夹就是用户自己创建的笔记本文件夹，笔记本文件夹下 `.sy` 后缀的文件用于保存文档数据，数据格式为 JSON

完整参考见 [`docs/WORKSPACE.md`](docs/WORKSPACE.md)。

### 支持通过第三方同步盘进行数据同步吗？

不支持——把 Dropbox、OneDrive 或类似的文件夹同步工具指向你的工作空间，可能会导致数据损坏。

接入自己的 S3、WebDAV 或本地文件系统存储是完全不同的另一回事，这是完整支持的，而且无需订阅。内核写入的是一个不可变的、按内容寻址的仓库，而不是实时同步文件本身。见 [`docs/SYNC.md`](docs/SYNC.md)。

### 思源是开源的吗？

是的，AGPL-3.0，和上游一致。注意如果你把它作为网络服务提供给他人使用，AGPL 第 13 条就会生效——你需要向他们提供源码。

上游自己的仓库，都是各自独立的项目：[界面和内核](https://github.com/siyuan-note/siyuan)、[Android](https://github.com/siyuan-note/siyuan-android)、[iOS](https://github.com/siyuan-note/siyuan-ios)、[鸿蒙](https://github.com/siyuan-note/siyuan-harmony)、[Chrome 剪藏扩展](https://github.com/siyuan-note/siyuan-chrome)。

### 如何升级？

没有自动更新——那套机制指向的是上游的发布基础设施，这个 fork 没有。拉取并重新构建即可：

```bash
git pull
cd app && pnpm install && pnpm run build && cd ..
cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..
```

如果还想合并上游的改动，先做合并：`git fetch upstream && git merge upstream/master`。预期会在注释处产生冲突——[`docs/FORK.md`](docs/FORK.md) 列出了这些冲突会出现在哪里。

### 有的块（比如在列表项中的段落块）找不到块标怎么办？

在列表项下的第一个子块是省略块标的。可以将光标移到这个块中，然后通过 <kbd>Ctrl+/</kbd> 触发它的块菜单。

### 数据仓库密钥遗失怎么办？

- 如果之前在多个设备上正确初始化过数据仓库密钥，那么该密钥在所有设备上都是相同的，可以在 <kbd>设置</kbd> - <kbd>账号与同步</kbd> - <kbd>本地数据仓库</kbd> - <kbd>数据仓库密钥</kbd> - <kbd>复制密钥字符串</kbd> 找回
- 如果之前没有正确配置过（比如多个设备上密钥不一致），或者所有设备都不可用、无法获取密钥字符串，可以按下面的步骤重置密钥：

  1. 手动备份数据，可以使用<kbd>导出 Data</kbd>，或者直接在文件系统上复制 <kbd>workspace/data/</kbd> 文件夹
  2. <kbd>设置</kbd> - <kbd>账号与同步</kbd> - <kbd>本地数据仓库</kbd> - <kbd>数据仓库密钥</kbd> - <kbd>重置数据仓库</kbd>
  3. 重新初始化数据仓库密钥，在一台设备上初始化密钥之后，其他设备导入该密钥
  4. 云端会使用新的同步目录，旧的同步目录不再可用，可以删除
  5. 已有的云端快照不再可用，可以删除

## 鸣谢

思源笔记是 [b3log](https://github.com/siyuan-note) 及其贡献者的作品。这个 fork 之所以存在，是因为原作足够好、也足够开放；所有软件层面的功劳都属于上游，而 [`docs/FORK.md`](docs/FORK.md) 里这些改动引入的任何 bug 都算在这个 fork 头上，与他们无关。请不要把这个 fork 的问题反馈给上游项目。

思源依赖了许多开源项目——见 `kernel/go.mod` 和 `app/package.json`。
