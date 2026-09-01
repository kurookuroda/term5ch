package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"x5ch-go/fivechbrowser"
)

// Task はDiscordへの転送待ちタスク。Ruby版のキュー要素(title/board_url/dat_file)に対応。
type Task struct {
	Title    string `json:"title"`
	BoardURL string `json:"board_url"`
	DatFile  string `json:"dat_file"`
}

// ThreadDataFetcher は browser.go の Browser.GetThreadData と同じシグネチャ。
// fivechbrowser.Browser がそのままこのインターフェースを満たす。
type ThreadDataFetcher interface {
	GetThreadData(t *fivechbrowser.ThreadInfo) ([]fivechbrowser.Post, error)
}

// HistoryUpdater は history.Manager が満たすべきインターフェース。
type HistoryUpdater interface {
	GetLastRead(boardURL, datFile string) int
	GetDiscordThreadID(boardURL, datFile string) string
	UpdateHistory(t fivechbrowser.ThreadInfo, resNum int, discordThreadID string)
}

// DiscordClient はDiscordへの送信を担う(discordパッケージ側の実装をここに注入する)。
// API呼び出し自体が失敗した場合は DiscordAPIError (かそれをラップしたerror) を返す想定。
type DiscordClient interface {
	CreateThread(title string) (string, error)
	SendMessage(discordThreadID string, post fivechbrowser.Post) error
}

// DiscordAPIError はDiscord API呼び出しレベルでの失敗(例: 429)を表す。
// discordパッケージの実装はこの型を返すかラップすることで、Workerが正しくリトライ分類できる。
type DiscordAPIError struct {
	Msg string
}

func (e *DiscordAPIError) Error() string { return "Discord API Error: " + e.Msg }

type Worker struct {
	browser ThreadDataFetcher
	discord DiscordClient
	history HistoryUpdater

	queueFile string

	// リトライ待機時間・メッセージ間隔はテスト時に短縮できるよう公開フィールドにしている。
	NetworkRetryDelay time.Duration
	DiscordRetryDelay time.Duration
	MessageInterval   time.Duration

	mu          sync.Mutex
	cond        *sync.Cond
	queue       []Task
	working     bool
	suspended   bool
	shutdown    bool
	currentTask *Task
	currentIdx  int
	totalMsgs   int
	lastError   string

	doneCh chan struct{}
}

func NewWorker(browser ThreadDataFetcher, discord DiscordClient, history HistoryUpdater, queueFile string) *Worker {
	w := &Worker{
		browser:           browser,
		discord:           discord,
		history:           history,
		queueFile:         queueFile,
		NetworkRetryDelay: 30 * time.Second,
		DiscordRetryDelay: 10 * time.Second,
		MessageInterval:   1 * time.Second,
		doneCh:            make(chan struct{}),
	}
	w.cond = sync.NewCond(&w.mu)
	w.loadQueue()
	go w.run()
	return w
}

func (w *Worker) loadQueue() {
	body, err := os.ReadFile(w.queueFile)
	if err != nil {
		return // ファイルが無ければ空スタート(Ruby版と同じ)
	}
	var q []Task
	if err := json.Unmarshal(body, &q); err != nil {
		return
	}
	w.mu.Lock()
	w.queue = q
	w.mu.Unlock()
}

// saveQueueLocked は呼び出し側が既に w.mu をロックしている前提で呼ぶ。
func (w *Worker) saveQueueLocked() {
	body, err := json.MarshalIndent(w.queue, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(w.queueFile, body, 0644) // Ruby版同様、保存失敗は握りつぶす
}

// Enqueue はタスクをキューに追加し、待機中のワーカーを起こす。
func (w *Worker) Enqueue(task Task) {
	w.mu.Lock()
	w.lastError = ""
	w.queue = append(w.queue, task)
	w.saveQueueLocked()
	w.mu.Unlock()
	w.cond.Signal()
}

// DeleteAt はキューの指定indexのタスクを削除する。
func (w *Worker) DeleteAt(index int) (Task, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if index < 0 || index >= len(w.queue) {
		return Task{}, false
	}
	removed := w.queue[index]
	w.queue = append(w.queue[:index], w.queue[index+1:]...)
	w.saveQueueLocked()
	return removed, true
}

// GetQueueList は現在のキューのスナップショットを返す。
func (w *Worker) GetQueueList() []Task {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Task, len(w.queue))
	copy(out, w.queue)
	return out
}

// Busy はキューに残りがあるか、処理中であるかを返す。
func (w *Worker) Busy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.queue) > 0 || w.working
}

func (w *Worker) RemainingThreads() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.queue)
}

func (w *Worker) LastError() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastError
}

// Suspend は処理を一時停止する。
func (w *Worker) Suspend() {
	w.mu.Lock()
	w.suspended = true
	w.mu.Unlock()
}

// Resume は一時停止を解除し、待機中のワーカーを起こす。
func (w *Worker) Resume() {
	w.mu.Lock()
	w.suspended = false
	w.mu.Unlock()
	w.cond.Broadcast()
}

// Kill はワーカーを停止させる。処理中のタスクがあればキューの先頭に戻してから保存する。
func (w *Worker) Kill() {
	w.mu.Lock()
	w.shutdown = true
	if w.working && w.currentTask != nil {
		w.queue = append([]Task{*w.currentTask}, w.queue...)
	}
	w.saveQueueLocked()
	w.mu.Unlock()
	w.cond.Broadcast()
}

// Done はワーカーgoroutineが完全に終了した際にcloseされるチャネルを返す。
func (w *Worker) Done() <-chan struct{} {
	return w.doneCh
}

// WaitUntilDone はキューが空になり、処理中のタスクも無くなるまでブロックする。
func (w *Worker) WaitUntilDone() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(w.queue) > 0 || w.working {
		w.cond.Wait()
	}
}

// StatusString はキュー/転送の進捗を表す色付き文字列を返す。Ruby版 status_string に対応。
func (w *Worker) StatusString() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.lastError != "" {
		return fmt.Sprintf(" \x1b[41m[%s]\x1b[0m", w.lastError)
	}

	isBusy := len(w.queue) > 0 || w.working
	if !isBusy {
		return ""
	}

	titleInfo := "準備中"
	if w.currentTask != nil {
		t := []rune(w.currentTask.Title)
		if len(t) > 8 {
			t = t[:8]
		}
		titleInfo = string(t) + "..."
	}

	progress := ""
	if w.working && w.totalMsgs > 0 {
		progress = fmt.Sprintf("(%d/%d)", w.currentIdx, w.totalMsgs)
	}

	queueInfo := ""
	if len(w.queue) > 0 {
		queueInfo = fmt.Sprintf(" [待機スレ:%d]", len(w.queue))
	}

	return fmt.Sprintf(" \x1b[33m[転送中:%s%s%s]\x1b[0m", titleInfo, progress, queueInfo)
}

func (w *Worker) run() {
	defer close(w.doneCh)
	for {
		var task *Task

		w.mu.Lock()
		for (len(w.queue) == 0 || w.suspended) && !w.shutdown {
			w.cond.Wait()
		}
		if !w.shutdown && len(w.queue) > 0 {
			t := w.queue[0]
			w.queue = w.queue[1:]
			task = &t
			w.currentTask = task
			w.saveQueueLocked()
		}
		shutdownNow := w.shutdown
		w.mu.Unlock()

		if shutdownNow && task == nil {
			return
		}
		if task == nil {
			continue
		}

		w.mu.Lock()
		w.working = true
		w.currentIdx = 0
		w.totalMsgs = 0
		w.mu.Unlock()

		err := w.processMirror(*task)
		if err != nil {
			w.handleTaskError(*task, err)
		} else {
			w.mu.Lock()
			w.lastError = ""
			w.mu.Unlock()
		}

		w.mu.Lock()
		w.currentTask = nil
		w.working = false
		w.mu.Unlock()
		w.cond.Broadcast()
	}
}

func (w *Worker) processMirror(task Task) error {
	t := fivechbrowser.ThreadInfo{Title: task.Title, BoardURL: task.BoardURL, DatFile: task.DatFile}

	posts, err := w.browser.GetThreadData(&t)
	if err != nil {
		if errors.Is(err, fivechbrowser.ErrThreadGone) {
			return nil // dat落ちは失敗ではなく単に諦める(Ruby版: postsがnilならreturnのみ)
		}
		return err
	}
	if len(posts) == 0 {
		return nil
	}

	discordThreadID := w.history.GetDiscordThreadID(task.BoardURL, task.DatFile)
	if discordThreadID == "" {
		id, err := w.discord.CreateThread(task.Title)
		if err != nil {
			return err
		}
		discordThreadID = id
		w.history.UpdateHistory(t, 0, discordThreadID)
	}

	lastRead := w.history.GetLastRead(task.BoardURL, task.DatFile)
	var newPosts []fivechbrowser.Post
	for _, p := range posts {
		if p.Num > lastRead {
			newPosts = append(newPosts, p)
		}
	}
	if len(newPosts) == 0 {
		return nil
	}

	w.mu.Lock()
	w.totalMsgs = len(newPosts)
	w.mu.Unlock()

	for idx, post := range newPosts {
		w.mu.Lock()
		shutdownNow := w.shutdown
		w.mu.Unlock()
		if shutdownNow {
			break
		}

		w.mu.Lock()
		w.currentIdx = idx + 1
		w.mu.Unlock()

		if err := w.discord.SendMessage(discordThreadID, post); err != nil {
			return err
		}
		w.history.UpdateHistory(t, post.Num, discordThreadID)

		time.Sleep(w.MessageInterval)
	}
	return nil
}

func (w *Worker) handleTaskError(task Task, err error) {
	var netErr net.Error
	var discordErr *DiscordAPIError

	switch {
	case errors.As(err, &netErr):
		w.setLastError("Network Error! Retry later...")
		time.Sleep(w.NetworkRetryDelay)
		w.requeue(task)
	case errors.As(err, &discordErr):
		w.setLastError(truncate(err.Error(), 20))
		time.Sleep(w.DiscordRetryDelay)
		w.requeue(task)
	default:
		w.setLastError(truncate(err.Error(), 20))
		// Ruby版同様、その他のエラーはログのみでrequeueしない
	}
}

func (w *Worker) setLastError(msg string) {
	w.mu.Lock()
	w.lastError = msg
	w.mu.Unlock()
}

func (w *Worker) requeue(task Task) {
	w.mu.Lock()
	w.queue = append(w.queue, task)
	w.saveQueueLocked()
	w.mu.Unlock()
	w.cond.Signal()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
