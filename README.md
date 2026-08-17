# dependabot-tool

GitHub Dependabot alerts をローカルの依存関係グラフ・ロックファイルと突き合わせて、対応の優先度や実際に手元のバージョンが影響を受けているかを分析するための CLI ツールです。[GitHub CLI (`gh`)](https://cli.github.com/) を利用して Dependabot alerts / pull requests を取得します。

## 前提条件

- Go 1.26 以降 ([mise](https://mise.jdx.dev/) を利用する場合は `mise install` で導入されます)
- [`gh`](https://cli.github.com/) がインストール済みで、対象リポジトリに対して `gh auth login` 済みであること

## インストール

```sh
go install github.com/iyuuya/dependabot-tool/cmd/dependabot-tool@latest
```

mise を使う場合:

```sh
mise run install
```

## 使い方

```sh
dependabot-tool <alerts|history> [flags]
```

### `alerts`

Dependabot alerts を取得し、ローカルのロックファイル（`poetry.lock` / JavaScript lockfiles / `Gemfile.lock`）や依存関係グラフと突き合わせて、深刻度・対応のしやすさ・影響有無などを一覧表示します。

```sh
dependabot-tool alerts [flags]
```

主なフラグ:

| フラグ | 説明 |
| --- | --- |
| `--repo` | `owner/repo`。省略時は `gh repo view` で自動判定 |
| `--state` | 取得する state（`open`/`fixed`/`dismissed` など）のカンマ区切り。`all` で全件。デフォルト: `open` |
| `--severity` | severity（`low`/`medium`/`high`/`critical`）のカンマ区切りで絞り込み |
| `--min-severity` | 指定した severity 以上だけ表示 |
| `--ecosystem` | ecosystem（`pip`/`npm`/`rubygems` など）のカンマ区切りで絞り込み |
| `--package` | パッケージ名のカンマ区切りで絞り込み |
| `--scope` | 依存スコープ（`runtime`/`development`）で絞り込み |
| `--gap` | パッチとのバージョン差（`major`/`minor`/`patch`/`other`/`up-to-date`/`unknown`）で絞り込み |
| `--ease` | 対応のしやすさ（`easy`/`medium`/`hard`/`?`）で絞り込み |
| `--fix-kind` | 対応の種類（`lock-only`/`direct`/`blocked`/`no-patch`/`unknown`）で絞り込み |
| `--affected-only` | 手元のバージョンが脆弱範囲に該当するものだけ表示 |
| `--format` | 出力形式（`table`/`tree`/`json`/`csv`）。デフォルト: `table` |
| `--summary` | table 出力に advisory の概要列を追加 |
| `--action` | table 出力に対応方法 (ACTION) の列を追加 |
| `--sort` | 並び順（`severity`/`ease`） |
| `--tree-depth` / `--tree-lines` | tree 出力時の逆依存ツリーの深さ・行数上限 |
| `--python-version` | 環境マーカー評価に使う Python バージョン |
| `--root` | ロックファイル探索のルート（省略時は git のトップレベル） |
| `--alerts-file` | `gh` を呼ばずに指定した JSON ファイルを使う（デバッグ用） |
| `--refresh` | キャッシュを無視して `gh api` を実行し直す |

### `history`（`histories` でも可）

直近にマージされた Dependabot PR の前後で Alert 数がどう変化したかを表示します。

```sh
dependabot-tool history [repo] [flags]
```

主なフラグ:

| フラグ | 説明 |
| --- | --- |
| `--days` | 直近 N 日にマージされた PR を表示（デフォルト: 14） |
| `--since` | `YYYY-MM-DD` 以降にマージされた PR を表示（`--days` より優先） |
| `--refresh` | キャッシュを無視して `gh api` を再実行し、結果を上書き |

## 対応エコシステム

現時点でローカルバージョンの解決に対応しているのは以下の組み合わせです。

- `pip`: `poetry.lock`
- `npm`: `bun.lock` / `package-lock.json` / `npm-shrinkwrap.json` / `pnpm-lock.yaml` / `yarn.lock`
- `rubygems`: `Gemfile.lock`（`Gemfile` / `gems.locked` も探索対象）

それ以外の ecosystem は `local_version` が `unsupported:<ecosystem>` として表示されます。

## ライセンス

[MIT](./LICENSE)
