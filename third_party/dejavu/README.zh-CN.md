# DejaVu

[English](README.md)

## 💡 简介

[DejaVu](https://github.com/siyuan-note/dejavu) 是[思源笔记](https://github.com/siyuan-note/siyuan)的数据快照和同步组件。

## 🍴 分叉说明

这是 [`github.com/siyuan-note/dejavu`](https://github.com/siyuan-note/dejavu) 的分叉，被整体引入到思源笔记分叉版的 `third_party/dejavu` 目录下，基于上游提交 `v0.0.0-20260715095305-8462fe30163c`。

它通过 `kernel/go.mod` 中一条已提交的 `replace` 指令接入内核，这是对“禁止提交 `replace` 指令”这一惯例的一次刻意例外，因为该路径位于仓库内部，因此在任何一次克隆和 CI 中都能正常解析。

**这是一个独立的 Go 模块**：在 `kernel/` 目录下运行 `go test ./...` 不会运行它的测试。请在 `third_party/dejavu` 目录内使用 `gofmt -l .`、`go vet ./...` 和 `go test -count=1 ./...` 来验证改动，其中 `test/sync` 是一次真实的双端同步模拟测试，是目前能拿到的最好的回归信号。

上游的修复不会自动合入，需要手动移植过来。

与上游的差异如下：

1. **块级 `.sy` 合并**（`sync_merge_blocks.go`）——两端都发生变动的文档会以最后一次同步的索引作为公共祖先按块合并，而不是一律产生冲突。本地树始终作为基准且绝不会被覆盖，因此漏检的最坏后果只是未能应用某个云端修改，绝不会丢失本地修改。只要结构发生变化、同一个块在两端都被修改、块中包含子块、块 ID 重复或为空，或者笔记本已加密，合并就会主动放弃并回退到上游原有的冲突处理方式。
2. **按同步目录命名空间化的 S3 对象键**（`cloud/s3.go`）——对象键前缀为 `siyuan/<dir>/repo/...`，使同一个存储桶可以容纳多个工作空间；名为 `main` 的目录保留历史上的桶根布局以兼容旧数据。`CreateRepo`/`RemoveRepo` 已经实现，`listRepos` 通过枚举键前缀来发现仓库。
3. **修复了 `getNotFound` 的数据竞争**（`cloud/s3.go`）——多个并发工作协程在没有加锁的情况下向同一个切片追加数据。丢失的追加会把云端实际缺失的分块误报为已存在，导致该分块永远不会被上传，云端仓库因此被悄悄地留下不完整的数据。

完整说明参见 [`FORK.md`](../../docs/FORK.md) 和 [`SYNC.md`](../../docs/SYNC.md)。

## ✨ 特性

* 类似 Git 的版本控制
* 文件分块去重
* 数据压缩
* AES 加密
* 云端同步和备份

⚠️ 注意

* 不支持文件夹
* 不支持权限属性
* 不支持符号链接

## 🎨 设计

设计参考自 [ArtiVC](https://github.com/InfuseAI/ArtiVC)。

### 实体

* `ID` 每个实体都通过 SHA-1 标识
* `Index` 文件列表，每次索引操作都生成一个新的索引
    * `memo` 索引备注
    * `created` 索引时间
    * `files` 文件列表
    * `count` 文件总数
    * `size` 文件列表总大小
* `File` 文件，实际的数据文件路径或者内容发生变动时生成一个新的文件
    * `path` 文件路径
    * `size` 文件大小
    * `updated` 最后更新时间
    * `chunks` 文件分块列表
* `Chunk` 文件块
    * `data` 实际的数据
* `Ref` 引用指向索引
    * `latest` 内置引用，自动指向最新的索引
    * `tag` 标签引用，手动指向指定的索引
* `Repo` 仓库

### 仓库

* `DataPath` 数据文件夹路径，实际的数据文件所在文件夹
* `Path` 仓库文件夹路径，仓库不保存在数据文件夹中，需要单独指定仓库文件夹路径

仓库文件夹结构如下：

```text
├─indexes
│      0531732dca85404e716abd6bb896319a41fa372b
│      19fc2c2e5317b86f9e048f8d8da2e4ed8300d8af
│      5f32d78d69e314beee36ad7de302b984da47ddd2
│      cbd254ca246498978d4f47e535bac87ad7640fe6
│
├─objects
│  ├─1e
│  │      0ac5f319f5f24b3fe5bf63639e8dbc31a52e3b
│  │
│  ├─56
│  │      322ccdb61feab7f2f76f5eb82006bd51da7348
│  │
│  ├─7e
│  │      dccca8340ebe149b10660a079f34a20f35c4d4
│  │
│  ├─83
│  │      a7d72fe9a071b696fc81a3dc041cf36cbde802
│  │
│  ├─85
│  │      26b9a7efde615b67b4666ae509f9fbc91d370b
│  │
│  ├─87
│  │      1355acd062116d1713e8f7f55969dbb507a040
│  │
│  ├─96
│  │      46ba13a4e8eabeca4f5259bfd7da41d368a1a6
│  │
│  ├─a5
│  │      5b8e6b9ccad3fc9b792d3d453a0793f8635b9f
│  │      b28787922f4e2a477b4f027e132aa7e35253d4
│  │
│  ├─be
│  │      c7a729d1b5f021f8eca0dd8b6ef689ad753567
│  │
│  ├─d1
│  │      324c714bde18442b5629a84a361b5e7528b14a
│  │
│  ├─f1
│  │      d7229171f4fa1c5eacb411995b16938a04f7f6
│  │
│  └─f7
│          ff9e8b7bb2e09b70935a5d785e0cc5d9d0abf0
│
└─refs
    │  latest
    │
    └─tags
            v1.0.0
            v1.0.1
```

## 📄 授权

DejaVu 使用 [GNU Affero 通用公共许可证, 版本 3](https://www.gnu.org/licenses/agpl-3.0.txt) 开源协议。

## 🙏 鸣谢

* [https://github.com/dustin/go-humanize](https://github.com/dustin/go-humanize) `MIT license`
* [https://github.com/klauspost/compress](https://github.com/klauspost/compress) `BSD-3-Clause license`
* [https://github.com/panjf2000/ants](https://github.com/panjf2000/ants) `MIT license`
* [https://github.com/InfuseAI/ArtiVC](https://github.com/InfuseAI/ArtiVC) `Apache-2.0 license`
* [https://github.com/restic/restic](https://github.com/restic/restic) `BSD-2-Clause license`
* [https://github.com/sabhiram/go-gitignore](https://github.com/sabhiram/go-gitignore) `MIT license`
