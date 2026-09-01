# x5ch-go

Ruby製CLI「x5ch」（5chブラウザ + Discord転送ツール）をGoに移植したもの。

## ビルド

```bash
go build -o x5ch ./cmd/x5ch
./x5ch
```

## go.mod の replace ディレクティブについて

開発時のサンドボックス環境が `golang.org` に到達できず、`go.sum` 検証も
無効化していたため、以下のパッケージは GitHub ミラー経由・`GOSUMDB=off` で
取得しています。

```
golang.org/x/text => github.com/golang/text
golang.org/x/term => github.com/golang/term
golang.org/x/sys  => github.com/golang/sys
```

**通常のネットワーク環境（`golang.org` に到達できる場所）で使う場合は、
この3行の `replace` を `go.mod` から削除して `go mod tidy` を実行すれば、
公式の `golang.org/x/...` を直接使う形に戻ります。** 動作は同一です。

```bash
# go.mod から replace 行を削除した後
go mod tidy
go build ./...
```

## Discord連携を使う場合

環境変数で設定します（Ruby版の `config.rb` に相当）。

```bash
export X5CH_DISCORD_BOT_TOKEN="..."
export X5CH_DISCORD_CHANNEL_ID="..."
```

未設定でも閲覧機能（読み取り専用ブラウザ部分）は問題なく動作します。

設定可能な環境変数一覧:

| 環境変数 | デフォルト |
|---|---|
| `X5CH_DISCORD_BOT_TOKEN` | (空、Discord機能無効) |
| `X5CH_DISCORD_CHANNEL_ID` | (空、Discord機能無効) |
| `X5CH_HISTORY_FILE` | `~/.x5ch_history.json` |
| `X5CH_QUEUE_FILE` | `~/.x5ch_queue.json` |
| `X5CH_LOCK_FILE` | `~/.x5ch.lock` |
| `X5CH_PID_FILE` | `~/.x5ch.pid` |

## 未検証の部分(要確認)

開発時のサンドボックス環境は `5ch.io` / `ff5ch.syoboi.jp` にネットワーク到達できず、
以下は実データでの動作確認ができていません。実際に `5ch.io` へ到達できる環境で
一度動作確認をお願いします。

- メニュー階層（カテゴリ→板→スレッド一覧）の実データでの表示・操作感
- スレッド本文の実際の閲覧（`pager` パッケージ自体は疑似端末で単体検証済み）
- 全板検索（`ff5ch.syoboi.jp` 経由）の実際の結果表示
- Discord連携（実際のBotトークンでのスレッド作成・メッセージ送信）

一方、以下は実バイナリ + 疑似端末(pty)でエンドツーエンドの動作を確認済みです。

- 多重起動防止(flock)・cronプロセスへのTERM→KILL引き継ぎ
- PIDファイルの作成・削除
- Ctrl+C受信時の保存終了処理（キュー保存→ロック解放→PIDファイル削除）
- `selector`（ページング・数字選択・絞り込み・タイムアウト再描画）の全操作
- `pager`（スクロール・全角文字幅対応）の全操作
- `TransferWorker`のキュー処理・リトライ分類・Kill時の差し戻し
- `discord.Manager`のAPI呼び出し（レート制限リトライ・長文分割）

## 構成

```
cmd/x5ch/          エントリポイント、ロック/PID管理、メインメニューループ
fivechbrowser/      HTTP取得・HTMLパース・キャッシュ(Ruby版 FiveChBrowser 相当)
history/            閲覧履歴のJSON永続化(Ruby版 HistoryManager 相当)
transfer/           Discord転送キューのワーカー(Ruby版 TransferWorker 相当)
pager/              スレッド本文のスクロール表示(Ruby版 ViPager 相当)
selector/           階層メニューのページング/選択UI(Ruby版 select_item 相当)
discord/            Discord REST API呼び出し(Ruby版 DiscordManager 相当)
```
