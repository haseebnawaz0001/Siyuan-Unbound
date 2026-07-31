<p align="center">
<img alt="SiYuan" src="app/stage/icon.png" width="128">
<br><br>
<a href="https://www.gnu.org/licenses/agpl-3.0.txt"><img src="https://img.shields.io/badge/license-AGPLv3-blue.svg" alt="License: AGPL v3"></a>
</p>

<p align="center">
<a href="README.md">English</a> | <a href="README.zh-CN.md">中文</a> | <strong>日本語</strong> | <a href="README.tr.md">Türkçe</a>
</p>

---

## これは何か

**SiYuan Unboundは[SiYuan](https://github.com/siyuan-note/siyuan)のフォークです**。SiYuanは、プライバシーを最優先とする個人の知識管理システムであり、ブロックレベルの参照とMarkdown WYSIWYGをサポートしています。書き直しではありません — コードのほぼすべては本家のものであり、本家からのマージを続けられる程度に差分を抑えています。

異なる点は4つです。

- **同期に、自分で用意したストレージを使う限りサブスクリプションは不要です。** S3互換オブジェクトストレージ、WebDAV、ローカルファイルシステムのディレクトリのいずれも、アカウントなしで同期できます。SiYuan自身がホストするサービスは引き続き有料のままです — 詳細は後述します。
- **テレメトリを削除しました。** ハードウェアのデバイスフィンガープリントも、お知らせの自動取得もありません。
- **既定の言語を英語にしています**。ソースコードのコメントも英語です。言語切り替え機能は引き続き動作し、すべての翻訳も同梱されています。
- **競合するドキュメントはブロック単位でマージされます**。一方が常に失われることはありません。

[`docs/FORK.md`](docs/FORK.md)には、それぞれの差分の内容、その理由、そして本家に対するリベース時に衝突する箇所が記録されています。

これは非公式フォークです。SiYuanプロジェクトによるサポート、承認、提携は一切なく、アプリストアへの掲載も、公開されたDockerイメージも、サポート窓口もありません。ライセンスは変更されておらず、AGPL-3.0のままです。

![Editing and block references](screenshots/feature0.png)

![Database views](screenshots/feature5-1.png)

## 特徴

- コンテンツブロック
  - ブロックレベルの参照と双方向リンク
  - カスタム属性
  - SQLクエリ埋め込み
  - プロトコル `siyuan://`
- エディタ
  - ブロックスタイル
  - Markdown WYSIWYG
  - リストアウトライン
  - ブロックズームイン
  - 百万字の大規模ドキュメント編集
  - 数学公式、チャート、フローチャート、ガントチャート、タイミングチャート、五線譜など
  - ウェブクリッピング
  - PDF注釈リンク
- エクスポート
  - ブロック参照と埋め込み
  - アセット付きの標準Markdown
  - PDF、Word、HTML
  - WeChat MP、Zhihu、Yuqueへのコピー
- データベース
  - テーブルビュー
- フラッシュカード間隔反復
- OpenAI APIを介したAIライティングとQ/Aチャット
- Tesseract OCR
- マルチタブ、ドラッグアンドドロップで分割画面
- テンプレートスニペット
- JavaScript/CSSスニペット
- 自分のS3 / WebDAV / ローカルストレージへ、アカウント不要で同期
- Dockerデプロイメント
- コミュニティマーケットプレイス

### 今も有料な機能

SiYuan自身のサーバー上で動くものはすべて、意図的に有料のまま残しています。公式クラウド同期、クラウド画像/アセットホスティング、クラウドリマインダー、エクスポート時のCDNアセット変換、そしてLiandiへの公開です。AGPL-3.0が対象とするのは、自分の手元にあるコピーを改変・実行する自由であり、他者のインフラを無料で使う権利ではありません。それらが必要な場合は[価格](https://b3log.org/siyuan/en/pricing.html)を参照してください。

## ビルドを入手する

ダウンロード配布は行っていません。自分でビルドしてください。

```bash
git clone git@github.com:haseebnawaz0001/Siyuan-Unbound.git
cd Siyuan-Unbound

cd app && pnpm install && pnpm run install:electron && pnpm run build && cd ..

cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..

./app/kernel/SiYuan-Kernel serve
```

Go 1.26、Node 24、pnpm 11.12.0、およびCコンパイラが必要です。カーネルはフロントエンドをHTTP経由で配信するため、最後の一行を実行するだけで動作するインストールになります — インストーラーは不要です。

デスクトップ用インストーラー、クロスコンパイル、Docker、モバイル、そしてこのリポジトリにある5つのビルド経路が互いに食い違う点については、**[`docs/BUILD.md`](docs/BUILD.md)** を参照してください。

### あるいはCIにビルドさせる

`.github/workflows/cd.yml` にはオーナー制限がないため、フォーク上でも実行できます。`*-alpha*`、`*-beta*`、`*-rc*` のいずれかに一致するタグをプッシュするか、ワークフローを手動でトリガーすると、Windows・macOS・Linux用のインストーラーがビルドされ、あなた自身のリポジトリのReleaseに添付されます。

注意点が2つあります。このワークフロー内のAndroidジョブは、あなたが管理していないリポジトリへプッシュしようとするため失敗します（デスクトップ用の成果物には影響しません）。また、このフォークではこの経路自体がまだ実際に検証されていません。

## セルフホスティング

イメージをビルドして実行します。

```bash
docker build -t siyuan-unbound .
```

このフォークはイメージを公開していませんし、公開できません。`.github/workflows/dockerimage.yml` は本家オーナー限定のゲートがかかっているため、公開ジョブはここでは実行されないからです。**[`docs/DEPLOY.md`](docs/DEPLOY.md)** ではコンテナの実行、Docker Compose、Unraid、TrueNASを扱っています。重要な注意点として、Dockerビルドは `sqlcipher` ビルドタグを含めていないため、暗号化されたノートブックが動作するとは想定しないでください。

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [FORK.md](docs/FORK.md) | このフォークが本家とどう違うか、その理由 |
| [SYNC.md](docs/SYNC.md) | 自分のS3、WebDAV、ローカルストレージに対する同期の設定方法 |
| [BUILD.md](docs/BUILD.md) | ソースからのビルド方法、全プラットフォーム |
| [DEPLOY.md](docs/DEPLOY.md) | Docker、Compose、Unraid、TrueNAS |
| [WORKSPACE.md](docs/WORKSPACE.md) | ワークスペースがディスク上でどう見えるか |
| [SY-FORMAT.md](docs/SY-FORMAT.md) | `.sy` ドキュメント形式 |
| [ENCRYPTED-NOTEBOOK.md](docs/ENCRYPTED-NOTEBOOK.md) | 暗号化ノートブックの仕組み |
| [API.md](docs/API.md) | カーネルのHTTP API |
| [CONTRIBUTING.md](.github/CONTRIBUTING.md) | 開発環境のセットアップと規約 |
| [AGENTS.md](AGENTS.md) | リポジトリガイド。コーディングエージェントがここで従うべきルールを含む |

## コマンドラインインターフェース

カーネルのバイナリはCLIも兼ねており、サーバーを起動しなくてもワークスペースのデータを直接読み書きできます。

```bash
# すべてのノートブックを一覧表示
siyuan notebook list -w ~/SiYuan

# JSON出力での全文検索
siyuan search "keyword" -w ~/SiYuan -f json

# アセットファイル内を検索（PDF/Word/Excel/txt など）
siyuan search "phrase" --asset -w ~/SiYuan
siyuan search "phrase" --asset --ext pdf --ext docx -w ~/SiYuan

# ドキュメントをMarkdownとしてエクスポート
siyuan export md --id <block-id> -w ~/SiYuan
```

| カテゴリ | コマンド |
|----------|----------|
| ノートと文書 | `notebook`, `document`, `dailynote` — CRUD とデイリーノート |
| コンテンツ | `block`, `attr`, `outline` — ブロックの読み書き、属性、アウトライン |
| メタデータ | `tag`, `bookmark`, `template` — タグ、ブックマーク、テンプレートスニペット |
| クエリ | `search`, `sql` — 全文・セマンティック・アセット内・SQL 検索 |
| 参照 | `ref` — バックリンクと言及 |
| インポート/エクスポート | `export`, `import`, `inbox` — Markdown, HTML, preview, Word, .sy.zip, Data, クラウド受信トレイ |
| データ管理 | `repo`, `history`, `sync` — スナップショット、バージョン、クラウド同期 |
| ユーティリティ | `asset`, `file` — リソースとファイルシステム |
| データベース | `database` — 属性ビュー管理 |
| サーバー | `serve` — カーネルのHTTPサーバーを起動 |
| ワークスペースとシステム | `workspace`, `system` — 一覧、確認、システム情報 |

`siyuan --help` を実行すると完全なコマンドツリーを確認できます。スクリプト向け出力には（デフォルトの `-f table` ではなく）`-f json` を使用します。変更を伴うほとんどのコマンドは `--dry-run` にも対応しており、適用せずに変更内容をプレビューできます。

バイナリは `<install-dir>/resources/kernel/SiYuan-Kernel` にあります。あるいは自分でビルドした場所です。`siyuan` として呼び出せるようにするには、`PATH` 上にシンボリックリンクを作成してください。

```bash
ln -s <install-dir>/resources/kernel/SiYuan-Kernel /usr/local/bin/siyuan
```

## アーキテクチャとエコシステム

| プロジェクト | 役割 |
|---|---|
| [lute](https://github.com/88250/lute) | エディタエンジン — Markdown/`.sy` のAST |
| [dejavu](third_party/dejavu) | データリポジトリと同期エンジン — **同梱フォーク**、詳細は[FORK.md](docs/FORK.md) §4を参照 |
| [riff](https://github.com/siyuan-note/riff) | 間隔反復のスケジューラ |
| [bazaar](https://github.com/siyuan-note/bazaar) | コミュニティマーケットプレイス |
| [petal](https://github.com/siyuan-note/petal) | プラグインAPI |
| [chrome](https://github.com/siyuan-note/siyuan-chrome) | ウェブクリッパー拡張機能 |
| [android](https://github.com/siyuan-note/siyuan-android) / [ios](https://github.com/siyuan-note/siyuan-ios) / [harmony](https://github.com/siyuan-note/siyuan-harmony) | gomobileカーネルをラップしたモバイルアプリ |

`dejavu` を除けば、これらはすべて本家が保守しているプロジェクトであり、このフォークでは変更せずにそのまま利用しています。モバイルアプリと拡張機能はカーネルのHTTP API経由で通信するため、このフォークに対しても動作しますが、それらをビルドすることはこのリポジトリの対象外です — 詳細は[`docs/BUILD.md`](docs/BUILD.md) §6を参照してください。

## FAQ

### SiYuanはどのようにデータを保存しますか？

データはワークスペースのdataフォルダーに保存されます。

- `assets` はすべての挿入されたアセットを保存するために使用されます
- `emojis` は絵文字画像を保存するために使用されます
- `snippets` はコードスニペットを保存するために使用されます
- `storage` はクエリ条件、レイアウト、フラッシュカードなどを保存するために使用されます
- `templates` はテンプレートスニペットを保存するために使用されます
- `widgets` はウィジェットを保存するために使用されます
- `plugins` はプラグインを保存するために使用されます
- `public` は公開データを保存するために使用されます
- 残りのフォルダーはユーザーが作成したノートブックフォルダーであり、ノートブックフォルダー内の `.sy` サフィックスのファイルはドキュメントデータを保存するために使用され、データ形式はJSONです

完全なリファレンスは[`docs/WORKSPACE.md`](docs/WORKSPACE.md)を参照してください。

### サードパーティの同期ディスクを介したデータ同期をサポートしていますか？

いいえ — Dropbox、OneDriveなどのフォルダ同期ツールをワークスペースに向けると、データが破損する可能性があります。

自分で用意したS3、WebDAV、ローカルファイルシステムのストレージへの接続はまったく別の話であり、サブスクリプション不要で完全にサポートされています。カーネルは稼働中のファイルを直接同期するのではなく、不変でコンテンツアドレス方式のリポジトリを書き出します。詳細は[`docs/SYNC.md`](docs/SYNC.md)を参照してください。

### SiYuanはオープンソースですか？

はい、本家と同じAGPL-3.0です。他者向けにネットワークサービスとして運用する場合はAGPL第13条が適用され、利用者にソースコードを提供する義務が生じる点に注意してください。

本家自身のリポジトリは、それぞれ別プロジェクトとして分かれています。[ユーザーインターフェースとカーネル](https://github.com/siyuan-note/siyuan)、[Android](https://github.com/siyuan-note/siyuan-android)、[iOS](https://github.com/siyuan-note/siyuan-ios)、[HarmonyOS](https://github.com/siyuan-note/siyuan-harmony)、[Chromeクリッピング拡張](https://github.com/siyuan-note/siyuan-chrome)。

### 新しいバージョンにアップグレードするにはどうすればよいですか？

自動更新機能はありません — その仕組みは本家のリリースインフラを参照するものであり、このフォークにはそれがないためです。次のコマンドでプルしてビルドし直してください。

```bash
git pull
cd app && pnpm install && pnpm run build && cd ..
cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..
```

本家の変更も取り込みたい場合は、先にマージしてください。`git fetch upstream && git merge upstream/master`。コメント部分で衝突が発生することが予想されます — その発生箇所は[`docs/FORK.md`](docs/FORK.md)に一覧があります。

### 一部のブロック（リスト項目内の段落ブロックなど）がブロックアイコンを見つけられない場合はどうすればよいですか？

リスト項目の最初のサブブロックはブロックアイコンが省略されています。このブロックにカーソルを移動し、<kbd>Ctrl+/</kbd> を使用してそのブロックメニューをトリガーできます。

### データリポジトリキーを紛失した場合はどうすればよいですか？

- データリポジトリキーが以前に複数のデバイスで正しく初期化されている場合、キーはすべてのデバイスで同じであり、<kbd>設定</kbd> - <kbd>アカウントと同期</kbd> - <kbd>ローカルデータリポジトリ</kbd> - <kbd>データリポジトリキー</kbd> - <kbd>キー文字列をコピー</kbd> で見つけることができます
- 以前に正しく構成されていない場合（たとえば、複数のデバイスでキーが一致しない場合）またはすべてのデバイスが使用できず、キー文字列を取得できない場合は、以下の手順でキーをリセットできます：

  1. データを手動でバックアップします。<kbd>データのエクスポート</kbd> を使用するか、ファイルシステム上で <kbd>ワークスペース/data/</kbd> フォルダーをコピーします
  2. <kbd>設定</kbd> - <kbd>アカウントと同期</kbd> - <kbd>ローカルデータリポジトリ</kbd> - <kbd>データリポジトリキー</kbd> - <kbd>データリポジトリをリセット</kbd>
  3. データリポジトリキーを再初期化します。1台のデバイスでキーを初期化した後、他のデバイスでキーをインポートします
  4. クラウドは新しい同期ディレクトリを使用します。古い同期ディレクトリは使用できなくなり、削除できます
  5. 既存のクラウドスナップショットは使用できなくなり、削除できます

## 謝辞

SiYuanは[b3log](https://github.com/siyuan-note)とそのコントリビューターによる成果物です。このフォークが存在するのは、その成果が優れておりオープンだからです。ソフトウェアとしての功績はすべて本家に帰属し、[`docs/FORK.md`](docs/FORK.md)に記した変更によって生じたバグはこちらの責任であり、本家の責任ではありません。このフォークに関する問題は、本家プロジェクトへは報告しないでください。

SiYuanは多くのオープンソースプロジェクトに依存しています — 詳細は `kernel/go.mod` と `app/package.json` を参照してください。
