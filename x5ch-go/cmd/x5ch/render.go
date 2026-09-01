package main

import (
	"fmt"

	"x5ch-go/fivechbrowser"
)

// threadItemState はスレッド一覧画面での表示用ラッパー。is_queued は selector.Item[T].Value に
// 持たせる必要があるため(Ruby版の item[:is_queued] 相当)、ThreadInfo単体ではなくこの構造体をTとする。
type threadItemState struct {
	Thread   fivechbrowser.ThreadInfo
	IsQueued bool
}

// renderRecentEntry は「★ 最近読んだスレッド」エントリの表示。
// Ruby版: display_name = "\e[1;36m#{title}\e[0m", prefix_mark=" ", info=""
func renderRecentEntry(title string) string {
	return fmt.Sprintf("  \x1b[1;36m%s\x1b[0m", title)
}

// renderCategoryOrBoardItem はカテゴリ/板一覧の表示(履歴の有無だけで分岐)。
// Ruby版の mode==:board / mode==:category の分岐ロジックに対応(表示ロジックは同一)。
// Ruby: prefix_mark = "\e[36m*\e[0m", display_name = "\e[36m#{title} (履歴あり)\e[0m"
func renderCategoryOrBoardItem(title string, hasHistory bool) string {
	if hasHistory {
		return fmt.Sprintf("\x1b[36m*\x1b[0m \x1b[36m%s (履歴あり)\x1b[0m", title)
	}
	return "  " + title
}

// renderThreadItem はスレッド一覧の表示。Ruby版 select_item の mode==:thread 分岐に対応。
func renderThreadItem(t fivechbrowser.ThreadInfo, isQueued bool) string {
	hasNew := t.HasNew()
	hasHistoryOrQueued := t.LastRead > 0 || isQueued

	mark := " "
	if hasNew {
		mark = "\x1b[31m+\x1b[0m"
	}

	displayName := t.Title
	prefixMark := " "

	if hasHistoryOrQueued {
		displayName = fmt.Sprintf("\x1b[36m%s\x1b[0m", t.Title)
		if !hasNew {
			prefixMark = "\x1b[36m✔\x1b[0m"
			mark = "\x1b[32m.\x1b[0m"
		}
	}

	var info string
	switch {
	case t.Ikioi > 0:
		info = fmt.Sprintf("%s (%d/%d)", mark, t.Count, int(t.Ikioi))
	case true: // Ruby版の elsif item[:count] は count=0でも真(Rubyの0は真)なので常にここに来る
		info = fmt.Sprintf("%s (%d)", mark, t.Count)
	}

	return fmt.Sprintf("%s%s %s", prefixMark, info, displayName)
}

// helpText は 'h' キー押下時のヘルプ画面。mode に応じて H の説明文だけが変わる。
// Ruby版 select_item 内 when 'h' に対応。
func helpText(mode string) string {
	base := "=== キー操作ヘルプ ===\r\n" +
		" 数字  : 決定 / Enter : 次ページ\r\n" +
		" m     : [NEW] 転送キューに追加\r\n" +
		" t     : [NEW] 転送キューの管理\r\n"

	switch mode {
	case "category":
		base += " H     : [NEW] 閲覧履歴の管理(削除)\r\n"
	case "thread":
		base += " H     : [NEW] 選択したスレッドの履歴を削除\r\n"
	}

	base += " s     : 検索 / r : リロード / q : 終了 / b : 戻る\r\n"
	base += " Ctrl+C: 待機列を保存して終了\r\n"
	return base
}
