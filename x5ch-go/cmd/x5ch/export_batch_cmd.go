package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"x5ch-go/fivechbrowser"
	"x5ch-go/history"
	"x5ch-go/transfer"
)

// exportBatchItem は export-batch の1スレ分の結果。成功/失敗を個別に持つことで、
// 1スレの失敗(dat落ち・通信エラー等)がバッチ全体を止めないようにする。
type exportBatchItem struct {
	OK        bool                        `json:"ok"`
	Error     string                      `json:"error,omitempty"`
	ErrorType string                      `json:"error_type,omitempty"`
	Source    *fivechbrowser.ExportSource `json:"source,omitempty"`
	Thread    *fivechbrowser.ExportThread `json:"thread,omitempty"`
	Posts     []fivechbrowser.ExportPost  `json:"posts,omitempty"`
}

type exportBatchResult struct {
	ExportedAt string            `json:"exported_at"`
	Threads    []exportBatchItem `json:"threads"`
}

type batchTarget struct {
	BoardURL string
	DatFile  string
	SinceNum int // --incremental時のみ非0
}

// runExportBatchCommand は `x5ch export-batch --source history|queue [--incremental]` を処理する。
func runExportBatchCommand(args []string) {
	fs := flag.NewFlagSet("export-batch", flag.ExitOnError)
	source := fs.String("source", "history", `対象の取得元 ("history" または "queue")`)
	incremental := fs.Bool("incremental", false, "各スレの既読位置(last_read)より後だけをエクスポートする(--source historyのみ有効)")
	fs.Parse(args)

	cfg := loadConfig()

	var targets []batchTarget
	switch *source {
	case "history":
		targets = loadTargetsFromHistory(cfg.HistoryFile, *incremental)
	case "queue":
		targets = loadTargetsFromQueue(cfg.QueueFile)
	default:
		fmt.Fprintf(os.Stderr, "不明な--source: %s (historyまたはqueueを指定)\n", *source)
		os.Exit(1)
	}

	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "対象のスレッドがありません")
		os.Exit(1)
	}

	browser := fivechbrowser.NewBrowser(cfg.UserAgent, nullHistory{}, cfg.CacheExpiration)

	items := make([]exportBatchItem, 0, len(targets))
	for i, t := range targets {
		fmt.Fprintf(os.Stderr, "[%d/%d] 取得中: %s %s\n", i+1, len(targets), t.BoardURL, t.DatFile)

		result, err := browser.ExportThreadData(t.BoardURL, t.DatFile, t.SinceNum)
		if err != nil {
			items = append(items, exportBatchItem{
				OK:        false,
				Error:     err.Error(),
				ErrorType: classifyErrorType(err),
				Source: &fivechbrowser.ExportSource{
					Provider: "5ch",
					BoardURL: t.BoardURL,
					DatFile:  t.DatFile,
				},
			})
			continue
		}

		items = append(items, exportBatchItem{
			OK:     true,
			Source: &result.Source,
			Thread: &result.Thread,
			Posts:  result.Posts,
		})
	}

	batch := exportBatchResult{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Threads:    items,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(batch); err != nil {
		fmt.Fprintln(os.Stderr, "JSON出力エラー:", err)
		os.Exit(1)
	}
}

func loadTargetsFromHistory(historyFile string, incremental bool) []batchTarget {
	mgr, err := history.NewManager(historyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "履歴読み込みエラー:", err)
		os.Exit(1)
	}

	recent := mgr.GetRecentThreads()
	targets := make([]batchTarget, 0, len(recent))
	for _, r := range recent {
		since := 0
		if incremental {
			since = r.LastRead
		}
		targets = append(targets, batchTarget{
			BoardURL: r.BoardURL,
			DatFile:  r.DatFile,
			SinceNum: since,
		})
	}
	return targets
}

// loadTargetsFromQueue はキューファイル(transfer.Workerが永続化するもの)から対象を読む。
// フィールド形状を独自定義せず transfer.Task をそのまま使うことで、
// 将来 Task の形が変わってもここが追従し忘れて壊れることがないようにしている。
func loadTargetsFromQueue(queueFile string) []batchTarget {
	body, err := os.ReadFile(queueFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		fmt.Fprintln(os.Stderr, "キューファイル読み込みエラー:", err)
		os.Exit(1)
	}

	var tasks []transfer.Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		fmt.Fprintln(os.Stderr, "キューファイル解析エラー:", err)
		os.Exit(1)
	}

	targets := make([]batchTarget, 0, len(tasks))
	for _, t := range tasks {
		targets = append(targets, batchTarget{BoardURL: t.BoardURL, DatFile: t.DatFile})
	}
	return targets
}
