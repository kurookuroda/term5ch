package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"x5ch-go/fivechbrowser"
)

// runExportCommand は `x5ch export <board_url> <dat_file> [--since-num N]` を処理する。
// 対話TUIのロック取得・rawモード設定を一切経由しない、非対話の使い捨てコマンドとして実装。
func runExportCommand(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	sinceNum := fs.Int("since-num", 0, "このレス番号より後だけ出力する(差分エクスポート用)")
	fs.Parse(args)

	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "使い方: x5ch export <board_url> <dat_file> [--since-num N]")
		fmt.Fprintln(os.Stderr, "例:     x5ch export https://mao.5ch.io/linux/ 1765829109.dat")
		os.Exit(1)
	}
	boardURL := rest[0]
	datFile := rest[1]

	cfg := loadConfig()
	// export専用の用途では閲覧履歴を一切参照・更新しないため、HistoryStoreはnilで構わない
	// (ExportThreadDataはhistoryフィールドを使用しない)。
	browser := fivechbrowser.NewBrowser(cfg.UserAgent, nullHistory{}, cfg.CacheExpiration)

	result, err := browser.ExportThreadData(boardURL, datFile, *sinceNum)
	if err != nil {
		if errors.Is(err, fivechbrowser.ErrThreadGone) {
			fmt.Fprintln(os.Stderr, "スレッドはdat落ちしています")
		} else if url := extractErrorURL(err); url != "" {
			fmt.Fprintf(os.Stderr, "エラー: %v (URL: %s)\n", err, url)
		} else {
			fmt.Fprintln(os.Stderr, "エラー:", err)
		}
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false) // 本文中の '&' '<' '>' 等をJSON側で余計にエスケープしない
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "JSON出力エラー:", err)
		os.Exit(1)
	}
}
