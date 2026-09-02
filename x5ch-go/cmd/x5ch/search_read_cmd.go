package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"

	"x5ch-go/fivechbrowser"
)

// nullHistory は永続化を一切行わないダミーのHistoryStore実装。
// search/read/export のような使い捨てCLIコマンドでは閲覧履歴を参照・更新する必要が無いが、
// fivechbrowser.SearchGlobal や GetThreadData(内部でDetectAndAddNextThreadも動く)は
// HistoryStore を実際に呼び出すため、nilを渡すとnilポインタ参照でクラッシュする。
// そのため必ずこのno-op実装を使う。
type nullHistory struct{}

func (nullHistory) GetLastRead(boardURL, datFile string) int     { return 0 }
func (nullHistory) Exists(boardURL, datFile string) bool         { return false }
func (nullHistory) AddNewThread(title, boardURL, datFile string) {}

// jsonEnvelope は search/read サブコマンド共通の出力封筒。成功/失敗を明示する。
type jsonEnvelope struct {
	OK        bool               `json:"ok"`
	Error     string             `json:"error,omitempty"`
	ErrorType string             `json:"error_type,omitempty"`
	Results   []jsonSearchResult `json:"results,omitempty"`
	Thread    *jsonThread        `json:"thread,omitempty"`
	Posts     []jsonPost         `json:"posts,omitempty"`
}

type jsonSearchResult struct {
	Title    string `json:"title"`
	Count    int    `json:"count"`
	BoardURL string `json:"board_url"`
	DatFile  string `json:"dat_file"`
	URL      string `json:"url"`
}

type jsonThread struct {
	Title    string `json:"title"`
	BoardURL string `json:"board_url"`
	DatFile  string `json:"dat_file"`
	Count    int    `json:"count"`
}

type jsonPost struct {
	Num     int    `json:"num"`
	Name    string `json:"name"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// classifyErrorType はエラーを "thread_gone" / "network" / "other" に分類する。
// Python側(edge-tts連携等)が分岐しやすいようにするための分類。
func classifyErrorType(err error) string {
	if errors.Is(err, fivechbrowser.ErrThreadGone) {
		return "thread_gone"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "network"
	}
	return "other"
}

func writeEnvelope(env jsonEnvelope) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(env)
}

// runSearchCommand は `x5ch search <keyword>` を処理する。
func runSearchCommand(args []string) {
	if len(args) < 1 {
		writeEnvelope(jsonEnvelope{OK: false, Error: "使い方: x5ch search <keyword>", ErrorType: "other"})
		os.Exit(1)
	}
	keyword := args[0]

	cfg := loadConfig()
	browser := fivechbrowser.NewBrowser(cfg.UserAgent, nullHistory{}, cfg.CacheExpiration)

	results, err := browser.SearchGlobal(keyword)
	if err != nil {
		writeEnvelope(jsonEnvelope{OK: false, Error: err.Error(), ErrorType: classifyErrorType(err)})
		os.Exit(1)
	}

	out := make([]jsonSearchResult, len(results))
	for i, r := range results {
		out[i] = jsonSearchResult{
			Title:    r.Title,
			Count:    r.Count,
			BoardURL: r.BoardURL,
			DatFile:  r.DatFile,
			URL:      r.URL,
		}
	}
	writeEnvelope(jsonEnvelope{OK: true, Results: out})
}

// runReadCommand は `x5ch read <board_url> <dat_file>` を処理する。
func runReadCommand(args []string) {
	if len(args) < 2 {
		writeEnvelope(jsonEnvelope{OK: false, Error: "使い方: x5ch read <board_url> <dat_file>", ErrorType: "other"})
		os.Exit(1)
	}
	boardURL := args[0]
	datFile := args[1]

	cfg := loadConfig()
	browser := fivechbrowser.NewBrowser(cfg.UserAgent, nullHistory{}, cfg.CacheExpiration)

	t := fivechbrowser.ThreadInfo{BoardURL: boardURL, DatFile: datFile}
	posts, err := browser.GetThreadData(&t)
	if err != nil {
		writeEnvelope(jsonEnvelope{OK: false, Error: err.Error(), ErrorType: classifyErrorType(err)})
		os.Exit(1)
	}

	jsonPosts := make([]jsonPost, len(posts))
	for i, p := range posts {
		jsonPosts[i] = jsonPost{Num: p.Num, Name: p.Name, Date: p.Date, Message: p.Message}
	}

	writeEnvelope(jsonEnvelope{
		OK: true,
		Thread: &jsonThread{
			Title:    t.Title,
			BoardURL: boardURL,
			DatFile:  datFile,
			Count:    len(posts),
		},
		Posts: jsonPosts,
	})
}
