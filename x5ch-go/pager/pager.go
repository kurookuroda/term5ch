package pager

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"x5ch-go/fivechbrowser"
	"x5ch-go/keyreader"
)

// ContentType はPagerに渡すコンテンツ項目の種類。Ruby版 content_list の :type に対応。
type ContentType int

const (
	ContentHeader ContentType = iota
	ContentUnreadMarker
	ContentSystemMsg
	ContentPost
	ContentSeparator
	ContentError
)

// ContentItem はPagerが受け取る1項目。用途に応じて使うフィールドが変わる。
type ContentItem struct {
	Type    ContentType
	Thread  *fivechbrowser.ThreadInfo
	Post    *fivechbrowser.Post // ContentPost のとき使用
	Message string              // ContentSystemMsg / ContentError のとき使用
}

type style int

const (
	styleNone style = iota
	styleBold
	styleRed
	styleDim
	styleBgBlue
)

type line struct {
	text  string
	style style
}

type lineInfo struct {
	thread *fivechbrowser.ThreadInfo
	res    int
}

// Result はPager終了時に返す、最後に見ていた位置の情報。
type Result struct {
	Thread *fivechbrowser.ThreadInfo
	Res    int
}

// Pager はスクロール可能なテキストビューア。Ruby版 ViPager に対応。
type Pager struct {
	lines     []line
	lineInfo  []lineInfo
	jumpIndex int
}

// New はコンテンツ項目からPagerを構築する。Ruby版 initialize + prepare_content に対応。
func New(content []ContentItem) *Pager {
	p := &Pager{}
	p.prepareContent(content)
	return p
}

func (p *Pager) addLine(text string, thread *fivechbrowser.ThreadInfo, res int, st style) {
	p.lines = append(p.lines, line{text: text, style: st})
	p.lineInfo = append(p.lineInfo, lineInfo{thread: thread, res: res})
}

func (p *Pager) prepareContent(content []ContentItem) {
	p.jumpIndex = 0

	for _, item := range content {
		switch item.Type {
		case ContentHeader:
			th := item.Thread
			p.addLine(strings.Repeat(" ", 60), th, 0, styleBgBlue)
			title := ""
			url := ""
			lastRead := 0
			if th != nil {
				title = th.Title
				url = th.URL
				lastRead = th.LastRead
			}
			p.addLine(fmt.Sprintf("【%s】", title), th, 0, styleBold)
			p.addLine(fmt.Sprintf(" URL: %s (既読: %d)", url, lastRead), th, 0, styleNone)
			p.addLine(strings.Repeat("=", 60), th, 0, styleNone)

		case ContentUnreadMarker:
			p.jumpIndex = maxInt(len(p.lines)-10, 0)
			p.addLine("▼▼▼ ここから未読 ▼▼▼", item.Thread, 0, styleRed)

		case ContentSystemMsg:
			p.addLine("  "+item.Message, item.Thread, 0, styleDim)

		case ContentPost:
			post := item.Post
			th := item.Thread
			header := fmt.Sprintf("%d : %s [%s]", post.Num, post.Name, post.Date)
			p.addLine(header, th, post.Num, styleBold)
			for _, l := range splitLines(post.Message) {
				p.addLine("  "+l, th, post.Num, styleNone)
			}
			p.addLine(strings.Repeat("-", 60), th, post.Num, styleDim)

		case ContentSeparator:
			p.addLine("", nil, 0, styleNone)
			p.addLine("      ( 次のスレッドへ続く )      ", nil, 0, styleDim)
			p.addLine("", nil, 0, styleNone)

		case ContentError:
			p.addLine(fmt.Sprintf("!!! %s !!!", item.Message), item.Thread, 0, styleRed)
		}
	}

	p.addLine("(End of Stream)", nil, 0, styleDim)
}

// splitLines は Ruby の String#each_line 相当(末尾の空行を含めない)に分割する。
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Start は端末をrawモードにし、キー入力によるスクロール操作を受け付ける。
// 'q'で終了し、その時点の閲覧位置を Result として返す。
//
// Ruby版との差異: Ruby版は STDIN.getch でキー入力の瞬間だけ一時的にrawモードにするが、
// Go版は開始時に一度だけrawモードにして終了時に復元する(一般的なGo TUIの作法に合わせた、
// 意図的な設計変更)。そのため出力は "\r\n" を明示的に使う必要がある。
func (p *Pager) Start(kr *keyreader.KeyReader, out io.Writer, fd int) (*Result, error) {
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("端末をrawモードにできません: %w", err)
	}
	defer term.Restore(fd, oldState)

	width, height, err := term.GetSize(fd)
	if err != nil {
		return nil, fmt.Errorf("端末サイズを取得できません: %w", err)
	}
	rows, cols := height, width

	currentLine := p.jumpIndex
	maxScroll := maxInt(len(p.lines)-rows, 0)
	if currentLine > maxScroll {
		currentLine = maxScroll
	}

	for {
		maxScroll = maxInt(len(p.lines)-rows, 0)
		p.render(out, currentLine, rows, cols)

		b, err := kr.ReadByte()
		if err != nil {
			return nil, err
		}

		currentContext := p.contextAt(currentLine, rows)

		if b >= '0' && b <= '9' {
			continue
		}

		switch b {
		case 'q':
			if currentContext == nil {
				return nil, nil
			}
			return &Result{Thread: currentContext.thread, Res: currentContext.res}, nil
		case 'j', '\r', '\n':
			if currentLine < maxScroll {
				currentLine++
			}
		case 'k':
			if currentLine > 0 {
				currentLine--
			}
		case 'f', ' ':
			currentLine = minInt(currentLine+rows-1, maxScroll)
		case 0x04: // Ctrl-D
			currentLine = minInt(currentLine+rows/2, maxScroll)
		case 'b':
			currentLine = maxInt(currentLine-(rows-1), 0)
		case 0x15: // Ctrl-U
			currentLine = maxInt(currentLine-rows/2, 0)
		case 'g':
			currentLine = 0
		case 'G':
			currentLine = maxScroll
		case 0x1b: // ESC
			b2, err := kr.ReadByte()
			if err != nil || b2 != '[' {
				continue
			}
			b3, err := kr.ReadByte()
			if err != nil {
				continue
			}
			switch b3 {
			case 'A':
				if currentLine > 0 {
					currentLine--
				}
			case 'B':
				if currentLine < maxScroll {
					currentLine++
				}
			}
		}
	}
}

// contextAt は表示中の最下行から上方向に走査し、最初に見つかったレス情報を返す。
func (p *Pager) contextAt(offset, rows int) *lineInfo {
	visibleBottom := minInt(offset+rows-1, len(p.lines)-1)
	for idx := visibleBottom; idx >= 0; idx-- {
		info := p.lineInfo[idx]
		if info.thread != nil && info.res > 0 {
			return &info
		}
	}
	return nil
}

func (p *Pager) render(out io.Writer, offset, rows, cols int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")

	end := minInt(offset+rows, len(p.lines))
	if offset > end {
		offset = end
	}

	for _, l := range p.lines[offset:end] {
		displayText := truncateDisplay(l.text, cols)
		switch l.style {
		case styleBold:
			fmt.Fprintf(out, "\x1b[1m%s\x1b[0m\r\n", displayText)
		case styleRed:
			fmt.Fprintf(out, "\x1b[31m%s\x1b[0m\r\n", displayText)
		case styleDim:
			fmt.Fprintf(out, "\x1b[2m%s\x1b[0m\r\n", displayText)
		case styleBgBlue:
			fmt.Fprintf(out, "\x1b[44;1m%s\x1b[0m\r\n", displayText)
		default:
			fmt.Fprintf(out, "%s\r\n", displayText)
		}
	}
}

// truncateDisplay は全角文字(表示幅2)を考慮した表示幅ベースの切り詰めを行う。
// Ruby版は text.length(文字数) > cols で判定しており全角文字の表示幅を考慮していなかったが、
// go-runewidth を使って表示幅ベースの正しい切り詰めに改善している(意図的な変更)。
func truncateDisplay(text string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= cols {
		return text
	}
	if cols <= 3 {
		return runewidth.Truncate(text, cols, "")
	}
	return runewidth.Truncate(text, cols, "...")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
