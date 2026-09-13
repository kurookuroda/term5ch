package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"x5ch-go/discord"
	"x5ch-go/fivechbrowser"
	"x5ch-go/history"
	"x5ch-go/keyreader"
	"x5ch-go/selector"
	"x5ch-go/transfer"
)

// menuEntry はメインメニューの1項目。「★最近読んだスレッド」特別枠と通常カテゴリを区別する。
type menuEntry struct {
	IsRecent bool
	Category fivechbrowser.Category
}

var errInterrupted = errors.New("interrupted")

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export":
			runExportCommand(os.Args[2:])
			return
		case "export-batch":
			runExportBatchCommand(os.Args[2:])
			return
		case "search":
			runSearchCommand(os.Args[2:])
			return
		case "read":
			runReadCommand(os.Args[2:])
			return
		}
	}

	cfg := loadConfig()

	lockFile, err := acquireLockWithHandoff(cfg.LockFile, cfg.PIDFile)
	if err != nil {
		fmt.Println("ロック取得エラー:", err)
		os.Exit(1)
	}
	defer lockFile.Close()

	if err := writePID(cfg.PIDFile); err != nil {
		fmt.Println("PIDファイル書き込みエラー:", err)
	}
	defer removePID(cfg.PIDFile)

	histMgr, err := history.NewManager(cfg.HistoryFile)
	if err != nil {
		fmt.Println("履歴読み込みエラー:", err)
		os.Exit(1)
	}

	browser := fivechbrowser.NewBrowser(cfg.UserAgent, histMgr, cfg.CacheExpiration)
	discordMgr := discord.NewManager(cfg.DiscordBotToken, cfg.DiscordChannelID)
	worker := transfer.NewWorker(browser, discordMgr, histMgr, cfg.QueueFile)

	kr := keyreader.New(os.Stdin)
	out := os.Stdout
	fd := int(os.Stdin.Fd())

	// raw モード中(selector/pager画面)のCtrl+Cはバイト0x03として各所で検知され
	// ActionInterrupt/errInterrupted として伝播する。raw モードを抜けている間
	// (メニュー間のネットワーク取得中)はOSのSIGINTとして届くため、ここでも捕捉する。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanupAndExit(worker, cfg.PIDFile, lockFile)
	}()

	err = runMenuLoop(browser, histMgr, worker, discordMgr, kr, out, fd)

	if errors.Is(err, errInterrupted) {
		cleanupAndExit(worker, cfg.PIDFile, lockFile)
		return
	}
	if err != nil {
		fmt.Println("\r\nエラー:", err)
	}

	if worker.Busy() {
		fmt.Println("\r\n\x1b[33m転送処理が残っています。すべて完了するまで待機します...\x1b[0m")
	}
	worker.WaitUntilDone()
	fmt.Print("\x1b[H\x1b[2J")
}

// cleanupAndExit はCtrl+C(SIGINT/SIGTERM含む)を受けた際、待機列を保存してから即座に終了する。
// Ruby版 rescue Interrupt ブロックに対応。
func cleanupAndExit(worker *transfer.Worker, pidFile string, lockFile *os.File) {
	fmt.Println("\r\n\x1b[31m保存して終了します。\x1b[0m")
	worker.Kill()
	<-worker.Done()
	removePID(pidFile)
	lockFile.Close()
	os.Exit(0)
}

// runMenuLoop はメインメニュー(カテゴリ一覧+最近読んだスレッド)のループ。Ruby版 main() に対応。
func runMenuLoop(browser *fivechbrowser.Browser, hist *history.Manager, worker *transfer.Worker, discordMgr *discord.Manager, kr *keyreader.KeyReader, out io.Writer, fd int) error {
	lastCatPage := 0

	for {
		cats, err := browser.GetMenu()
		if err != nil {
			fmt.Fprintln(out, "メニュー取得エラー:", err)
			time.Sleep(2 * time.Second)
			continue
		}

		items := make([]selector.Item[menuEntry], 0, len(cats)+1)
		items = append(items, selector.Item[menuEntry]{
			Text:  renderRecentEntry("★ 最近読んだスレッド"),
			Value: menuEntry{IsRecent: true},
		})
		for _, c := range cats {
			hasHistory := hist.HasHistoryInCategory(c.Boards)
			items = append(items, selector.Item[menuEntry]{
				Text:  renderCategoryOrBoardItem(c.Title, hasHistory),
				Value: menuEntry{Category: c},
			})
		}

		catResult, err := selector.Run(kr, out, fd, selector.Config[menuEntry]{
			Title:       "メインメニュー",
			Items:       items,
			StartPage:   lastCatPage,
			StatusLine:  worker.StatusString,
			SearchLabel: "全スレ検索",
			OnGlobalSearch: func(keyword string) bool {
				return runGlobalSearch(browser, hist, worker, discordMgr, keyword, kr, out, fd)
			},
			OnQueueManage:   func(sio *selector.IO) bool { return manageQueue(sio, worker) },
			OnHistoryManage: func(sio *selector.IO) bool { return manageHistory(sio, hist) },
			HelpText:        helpText("category"),
		})
		if err != nil {
			return err
		}
		lastCatPage = catResult.Page

		switch catResult.Action {
		case selector.ActionQuit:
			return nil
		case selector.ActionBack:
			continue
		case selector.ActionInterrupt:
			return errInterrupted
		}

		entry := catResult.Value
		if entry.IsRecent {
			recent := hist.GetRecentThreads()
			if len(recent) == 0 {
				fmt.Fprintln(out, "履歴がありません")
				time.Sleep(1 * time.Second)
				continue
			}
			showRecentStream(browser, hist, recent, kr, out, fd)
			continue
		}

		if err := runBoardLoop(browser, hist, worker, discordMgr, entry.Category, kr, out, fd); err != nil {
			return err
		}
	}
}

func runBoardLoop(browser *fivechbrowser.Browser, hist *history.Manager, worker *transfer.Worker, discordMgr *discord.Manager, cat fivechbrowser.Category, kr *keyreader.KeyReader, out io.Writer, fd int) error {
	lastBoardPage := 0

	for {
		items := make([]selector.Item[fivechbrowser.Board], 0, len(cat.Boards))
		for _, b := range cat.Boards {
			hasHistory := hist.HasHistoryInBoard(b.URL)
			items = append(items, selector.Item[fivechbrowser.Board]{
				Text:  renderCategoryOrBoardItem(b.Title, hasHistory),
				Value: b,
			})
		}

		boardResult, err := selector.Run(kr, out, fd, selector.Config[fivechbrowser.Board]{
			Title:          cat.Title,
			Items:          items,
			StartPage:      lastBoardPage,
			StatusLine:     worker.StatusString,
			SearchLabel:    "絞り込み",
			SupportsReload: true,
			OnQueueManage:  func(sio *selector.IO) bool { return manageQueue(sio, worker) },
			HelpText:       helpText("board"),
		})
		if err != nil {
			return err
		}

		switch boardResult.Action {
		case selector.ActionQuit:
			return nil
		case selector.ActionBack:
			return nil
		case selector.ActionInterrupt:
			return errInterrupted
		case selector.ActionReload:
			continue
		}

		lastBoardPage = boardResult.Page
		board := boardResult.Value

		if err := runThreadLoop(browser, hist, worker, discordMgr, board, kr, out, fd); err != nil {
			return err
		}
	}
}

func runThreadLoop(browser *fivechbrowser.Browser, hist *history.Manager, worker *transfer.Worker, discordMgr *discord.Manager, board fivechbrowser.Board, kr *keyreader.KeyReader, out io.Writer, fd int) error {
	lastThreadPage := 0
	forceReload := false

	for {
		fmt.Fprintln(out, "スレッド一覧取得中...")
		threads, err := browser.GetThreads(&board, forceReload)
		forceReload = false
		if err != nil {
			fmt.Fprintln(out, "スレッド一覧取得エラー:", err)
			time.Sleep(2 * time.Second)
			return nil
		}

		items := make([]selector.Item[threadItemState], len(threads))
		for i, t := range threads {
			state := threadItemState{Thread: t}
			items[i] = selector.Item[threadItemState]{
				Text:  renderThreadItem(state.Thread, state.IsQueued),
				Value: state,
			}
		}

		threadResult, err := selector.Run(kr, out, fd, selector.Config[threadItemState]{
			Title:          board.Title,
			Items:          items,
			StartPage:      lastThreadPage,
			StatusLine:     worker.StatusString,
			SearchLabel:    "絞り込み",
			SupportsReload: true,
			CanEnqueue:     enqueueGuard(discordMgr),
			OnEnqueue:      enqueueHandler(worker, hist),
			OnHistoryDeleteItem: func(sio *selector.IO, idx int, item selector.Item[threadItemState]) (selector.Item[threadItemState], bool) {
				return deleteHistoryForThread(sio, hist, item.Value)
			},
			OnQueueManage: func(sio *selector.IO) bool { return manageQueue(sio, worker) },
			HelpText:      helpText("thread"),
		})
		if err != nil {
			return err
		}
		lastThreadPage = threadResult.Page

		switch threadResult.Action {
		case selector.ActionQuit:
			return nil
		case selector.ActionBack:
			return nil
		case selector.ActionInterrupt:
			return errInterrupted
		case selector.ActionReload:
			forceReload = true
			continue
		}

		t := threadResult.Value.Thread
		showThread(browser, hist, t, kr, out, fd)
	}
}

// runGlobalSearch はカテゴリ画面からの全板検索('s')。結果一覧を別のselector.Runで表示し、
// 選択されたスレッドを showThread で読む、というネストしたループ。Ruby版のselect_item再帰呼び出しに対応。
// 戻り値はCtrl+Cによる中断有無。
func runGlobalSearch(browser *fivechbrowser.Browser, hist *history.Manager, worker *transfer.Worker, discordMgr *discord.Manager, keyword string, kr *keyreader.KeyReader, out io.Writer, fd int) bool {
	results, err := browser.SearchGlobal(keyword)
	if err != nil {
		fmt.Fprintln(out, "検索エラー:", err)
		waitForKey(kr)
		return false
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "見つかりませんでした。")
		waitForKey(kr)
		return false
	}

	lastPage := 0
	for {
		items := make([]selector.Item[threadItemState], len(results))
		for i, t := range results {
			state := threadItemState{Thread: t}
			items[i] = selector.Item[threadItemState]{
				Text:  renderThreadItem(state.Thread, state.IsQueued),
				Value: state,
			}
		}

		result, err := selector.Run(kr, out, fd, selector.Config[threadItemState]{
			Title:         fmt.Sprintf("全板検索結果: %s", keyword),
			Items:         items,
			StartPage:     lastPage,
			StatusLine:    worker.StatusString,
			SearchLabel:   "絞り込み",
			CanEnqueue:    enqueueGuard(discordMgr),
			OnEnqueue:     enqueueHandler(worker, hist),
			OnQueueManage: func(sio *selector.IO) bool { return manageQueue(sio, worker) },
			HelpText:      helpText("thread"),
		})
		if err != nil {
			return false
		}
		lastPage = result.Page

		switch result.Action {
		case selector.ActionQuit, selector.ActionBack:
			return false
		case selector.ActionInterrupt:
			return true
		case selector.ActionSelected:
			showThread(browser, hist, result.Value.Thread, kr, out, fd)
		}
	}
}

// enqueueGuard は'm'キー押下時の事前チェック(Discordトークン未設定なら拒否)。
func enqueueGuard(discordMgr *discord.Manager) func() (bool, string) {
	return func() (bool, string) {
		if !discordMgr.Enabled() {
			return false, "\x1b[31m[Error] Discordトークン/チャンネルIDが未設定です\x1b[0m"
		}
		return true, ""
	}
}

// enqueueHandler は'm'キーでの実際のキュー追加処理。selectorから渡される item を直接使うため、
// (絞り込み中でも)呼び出し元のスライスのインデックスを気にする必要がない。
func enqueueHandler(worker *transfer.Worker, hist *history.Manager) func(sio *selector.IO, idx int, item selector.Item[threadItemState]) (selector.Item[threadItemState], bool) {
	return func(sio *selector.IO, idx int, item selector.Item[threadItemState]) (selector.Item[threadItemState], bool) {
		state := item.Value
		worker.Enqueue(transfer.Task{
			Title:    state.Thread.Title,
			BoardURL: state.Thread.BoardURL,
			DatFile:  state.Thread.DatFile,
		})
		hist.AddNewThread(state.Thread.Title, state.Thread.BoardURL, state.Thread.DatFile)
		state.Thread.LastRead = 0
		state.IsQueued = true
		sio.Println(">> キューに追加: " + state.Thread.Title)
		time.Sleep(500 * time.Millisecond)
		return selector.Item[threadItemState]{Text: renderThreadItem(state.Thread, state.IsQueued), Value: state}, true
	}
}

func deleteHistoryForThread(sio *selector.IO, hist *history.Manager, state threadItemState) (selector.Item[threadItemState], bool) {
	if state.Thread.LastRead > 0 {
		if hist.DeleteThread(state.Thread.BoardURL, state.Thread.DatFile) {
			state.Thread.LastRead = 0
			state.IsQueued = false
			sio.Println(">> 履歴を削除しました: " + state.Thread.Title)
			time.Sleep(1 * time.Second)
		} else {
			sio.Println("削除に失敗しました")
			time.Sleep(1 * time.Second)
		}
	} else {
		sio.Println("履歴がない(未読の)スレッドです")
		time.Sleep(500 * time.Millisecond)
	}
	return selector.Item[threadItemState]{Text: renderThreadItem(state.Thread, state.IsQueued), Value: state}, true
}
