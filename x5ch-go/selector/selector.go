package selector

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// Item はリストの1項目。Text は事前レンダリング済みの表示文字列(色・マーク付け済み)、
// Value は呼び出し側の実データ(fivechbrowser.ThreadInfo等)を保持する。
// ジェネリクスにより、選択結果は常にこの Value を直接返せるため、
// 「フィルタ後のインデックスを元のリストへ逆引きする」という不安定な処理が不要になる。
type Item[T any] struct {
	Text  string
	Value T
}

// Action は Run() の終了理由。
type Action int

const (
	ActionSelected Action = iota
	ActionQuit
	ActionBack
	ActionReload
	ActionInterrupt // Ctrl+C (Ruby版の raise Interrupt に対応)
)

// Result は Run() の戻り値。
type Result[T any] struct {
	Action Action
	Value  T // ActionSelected のとき、選択された Item.Value
	Page   int
}

// Config は Run() の挙動を決める設定。Ruby版 select_item の mode 引数による分岐を、
// 個々のコールバックの有無で表現する(nilなら該当キーは無効/no-op)。
type Config[T any] struct {
	Title     string
	Items     []Item[T]
	StartPage int
	PageSize  int

	StatusLine func() string

	SearchLabel    string
	OnGlobalSearch func(keyword string) (interrupted bool)

	SupportsReload bool

	HelpText string

	OnQueueManage       func(io *IO) (interrupted bool)
	OnHistoryManage     func(io *IO) (interrupted bool)
	OnHistoryDeleteItem func(io *IO, idx int, item Item[T]) (Item[T], bool)

	// CanEnqueue は'm'キー押下時、番号入力プロンプトを出す前のチェック。
	// nilなら常に許可。falseを返すとメッセージ表示のみでプロンプトを出さない
	// (Ruby版の「Token未設定なら即エラー表示」に対応)。
	CanEnqueue func() (ok bool, errMsg string)
	OnEnqueue  func(io *IO, idx int, item Item[T]) (Item[T], bool)
}

// IO はコールバックに渡される、raw モードを維持したまま行入力・単キー入力を行うためのヘルパー。
type IO struct {
	kr  *keyReader
	out io.Writer
}

func (io2 *IO) Print(s string)   { fmt.Fprint(io2.out, s) }
func (io2 *IO) Println(s string) { fmt.Fprint(io2.out, s+"\r\n") }

// ReadKey は1バイト読めるまでブロックする(タイムアウト無し)。
func (io2 *IO) ReadKey() (byte, error) {
	select {
	case b := <-io2.kr.ch:
		return b, nil
	case err := <-io2.kr.errCh:
		return 0, err
	}
}

// ReadLine は raw モードのまま、Enterまで1行を手動エコー・バックスペース処理しながら読む。
// Ruby版の gets() 相当(rawモードを一時解除する代わりに、Go版は手動でエコーする)。
func (io2 *IO) ReadLine() (string, error) {
	var buf []rune
	for {
		b, err := io2.ReadKey()
		if err != nil {
			return "", err
		}
		switch {
		case b == '\r' || b == '\n':
			io2.Print("\r\n")
			return string(buf), nil
		case b == 0x7f || b == 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				io2.Print("\b \b")
			}
		case b == 0x03:
			return "", errInterrupt
		default:
			buf = append(buf, rune(b))
			io2.Print(string(rune(b)))
		}
	}
}

type keyReader struct {
	ch    chan byte
	errCh chan error
}

func startKeyReader(r io.Reader) *keyReader {
	kr := &keyReader{ch: make(chan byte), errCh: make(chan error, 1)}
	br := bufio.NewReader(r)
	go func() {
		for {
			b, err := br.ReadByte()
			if err != nil {
				kr.errCh <- err
				return
			}
			kr.ch <- b
		}
	}()
	return kr
}

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

const errInterrupt = sentinelErr("interrupt")

// ErrInterrupted はCtrl+C押下を表すセンチネルエラー。ReadKey/ReadLineが検知した際に返す。
// 呼び出し側(main.goのmanageQueue等)は errors.Is で判定し、trueを呼び出し元に伝播させることで
// ネストした入力ループの最中でもCtrl+Cが最上位まで届くようにする。
var ErrInterrupted error = errInterrupt

// Run はページング・数字選択・検索・各種アクションキーを処理するメインループ。
// Ruby版 select_item に対応。
func Run[T any](in io.Reader, out io.Writer, fd int, cfg Config[T]) (Result[T], error) {
	var zero T
	if len(cfg.Items) == 0 {
		return Result[T]{Action: ActionBack, Value: zero, Page: 0}, nil
	}
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return Result[T]{}, fmt.Errorf("端末をrawモードにできません: %w", err)
	}
	defer term.Restore(fd, oldState)

	kr := startKeyReader(in)
	sio := &IO{kr: kr, out: out}

	originalItems := cfg.Items
	filteredItems := cfg.Items
	filterKeyword := ""
	inputBuffer := ""
	currentPage := cfg.StartPage
	needsFullRedraw := true

	for {
		totalPages := (len(filteredItems) + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}
		if currentPage >= totalPages {
			currentPage = 0
		}
		if currentPage < 0 {
			currentPage = totalPages - 1
		}

		startIdx := currentPage * pageSize
		endIdx := startIdx + pageSize
		if endIdx > len(filteredItems) {
			endIdx = len(filteredItems)
		}
		viewItems := filteredItems[startIdx:endIdx]

		displayTitle := cfg.Title
		if filterKeyword != "" {
			displayTitle += fmt.Sprintf(" (検索: %s)", filterKeyword)
		}
		status := ""
		if cfg.StatusLine != nil {
			status = cfg.StatusLine()
		}
		headerStr := fmt.Sprintf("--- %s (%d/%d)%s ---", displayTitle, currentPage+1, totalPages, status)

		lastIdx := len(viewItems) - 1
		if lastIdx < 0 {
			lastIdx = 0
		}
		validRange := fmt.Sprintf("%d-%d", startIdx, startIdx+lastIdx)

		searchLabel := ",s"
		if cfg.SearchLabel != "" {
			searchLabel = ",s(" + cfg.SearchLabel + ")"
		}
		promptKeys := fmt.Sprintf("[%s,Enter,p,b%s,h,q,t", validRange, searchLabel)
		if cfg.OnEnqueue != nil {
			promptKeys += ",r,m,H"
		} else if cfg.OnHistoryManage != nil {
			promptKeys += ",H"
		}
		promptKeys += "] > "

		if needsFullRedraw {
			fmt.Fprint(out, "\x1b[H\x1b[2J")
			fmt.Fprint(out, headerStr+"\r\n")
			for i, item := range viewItems {
				fmt.Fprintf(out, "[%d] %s\r\n", startIdx+i, item.Text)
			}
			fmt.Fprint(out, promptKeys+inputBuffer)
			needsFullRedraw = false
		} else {
			fmt.Fprint(out, "\x1b[H"+headerStr+"\x1b[K")
			promptRow := len(viewItems) + 2
			fmt.Fprintf(out, "\x1b[%d;1H", promptRow)
			fmt.Fprint(out, promptKeys+inputBuffer)
		}

		var b byte
		var readErr error
		select {
		case b = <-kr.ch:
		case readErr = <-kr.errCh:
		case <-time.After(1 * time.Second):
			continue
		}
		if readErr != nil {
			return Result[T]{}, readErr
		}

		if b == 0x03 {
			return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
		}

		if b >= '0' && b <= '9' {
			inputBuffer += string(rune(b))
			continue
		}

		if b == '\r' || b == '\n' {
			if inputBuffer == "" {
				currentPage++
			} else {
				idx, _ := strconv.Atoi(inputBuffer)
				inputBuffer = ""
				if idx >= 0 && idx < len(filteredItems) {
					return Result[T]{Action: ActionSelected, Value: filteredItems[idx].Value, Page: currentPage}, nil
				}
			}
			needsFullRedraw = true
			continue
		}

		if b == 0x7f || b == 0x08 {
			if len(inputBuffer) > 0 {
				inputBuffer = inputBuffer[:len(inputBuffer)-1]
			}
			continue
		}

		inputBuffer = ""

		switch b {
		case 'q':
			return Result[T]{Action: ActionQuit, Page: currentPage}, nil
		case 'b':
			return Result[T]{Action: ActionBack, Page: currentPage}, nil
		case 'r':
			needsFullRedraw = true
			if cfg.SupportsReload {
				return Result[T]{Action: ActionReload, Page: currentPage}, nil
			}
		case 't':
			if cfg.OnQueueManage != nil {
				if cfg.OnQueueManage(sio) {
					return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
				}
			}
			needsFullRedraw = true
		case 'H':
			if cfg.OnHistoryManage != nil {
				if cfg.OnHistoryManage(sio) {
					return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
				}
				needsFullRedraw = true
			} else if cfg.OnHistoryDeleteItem != nil {
				sio.Print("\r\n履歴を削除する番号を入力 > ")
				line, err := sio.ReadLine()
				if errors.Is(err, ErrInterrupted) {
					return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
				}
				if err == nil {
					if idx, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil {
						if idx >= 0 && idx < len(filteredItems) {
							if updated, ok := cfg.OnHistoryDeleteItem(sio, idx, filteredItems[idx]); ok {
								filteredItems[idx] = updated
							}
						}
					}
				}
				needsFullRedraw = true
			}
		case 'm':
			if cfg.OnEnqueue != nil {
				if cfg.CanEnqueue != nil {
					if ok, msg := cfg.CanEnqueue(); !ok {
						sio.Println("\r\n" + msg)
						time.Sleep(1 * time.Second)
						needsFullRedraw = true
						continue
					}
				}
				sio.Print("\r\n転送する番号を入力 > ")
				line, err := sio.ReadLine()
				if errors.Is(err, ErrInterrupted) {
					return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
				}
				if err == nil {
					if idx, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil {
						if idx >= 0 && idx < len(filteredItems) {
							if updated, ok := cfg.OnEnqueue(sio, idx, filteredItems[idx]); ok {
								filteredItems[idx] = updated
							}
						}
					}
				}
				needsFullRedraw = true
			}
		case 'h':
			fmt.Fprint(out, "\x1b[H\x1b[2J")
			fmt.Fprint(out, cfg.HelpText)
			if hb, _ := sio.ReadKey(); hb == 0x03 {
				return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
			}
			needsFullRedraw = true
		case 's':
			sio.Print("\r\n検索キーワード > ")
			keyword, err := sio.ReadLine()
			if errors.Is(err, ErrInterrupted) {
				return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
			}
			if err != nil || strings.TrimSpace(keyword) == "" {
				needsFullRedraw = true
				continue
			}
			keyword = strings.TrimSpace(keyword)

			if cfg.OnGlobalSearch != nil {
				if cfg.OnGlobalSearch(keyword) {
					return Result[T]{Action: ActionInterrupt, Page: currentPage}, nil
				}
				needsFullRedraw = true
			} else {
				filterKeyword = keyword
				var newFiltered []Item[T]
				for _, it := range originalItems {
					if strings.Contains(it.Text, keyword) {
						newFiltered = append(newFiltered, it)
					}
				}
				currentPage = 0
				if len(newFiltered) == 0 {
					sio.Println("該当なし")
					sio.ReadKey()
					filteredItems = originalItems
					filterKeyword = ""
				} else {
					filteredItems = newFiltered
				}
				needsFullRedraw = true
			}
		case 'p':
			currentPage--
			needsFullRedraw = true
		}
	}
}
