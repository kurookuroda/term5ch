package selector

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// isAnsiTerminator は CSI シーケンス(\x1b[...X)の終端文字(英字)かどうかを判定する。
func isAnsiTerminator(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// skipAnsiEscape は runes[i] が ESC '[' から始まるCSIシーケンスの先頭のとき、
// その終端(終端文字を含む)の次のインデックスを返す。シーケンスでなければ i をそのまま返す。
func skipAnsiEscape(runes []rune, i int) int {
	if runes[i] != 0x1b || i+1 >= len(runes) || runes[i+1] != '[' {
		return i
	}
	j := i + 2
	for j < len(runes) && !isAnsiTerminator(runes[j]) {
		j++
	}
	if j < len(runes) {
		j++ // 終端文字自体を含める
	}
	return j
}

// truncateLine は色付けなどのANSIエスケープシーケンスを保持したまま、表示幅(全角=2として計算)が
// maxWidth を超える行を "..." 付きで切り詰める。ターミナル幅より長い行を1行のまま出力すると
// ターミナル側の自動折り返しで物理行数がずれ、部分再描画(\x1b[<行>;1H によるプロンプト行への
// ジャンプ)の行位置計算が狂う原因になるため、selector.Run内の全出力行(ヘッダー・各項目)に適用する。
// pager.go の truncateDisplay と同じ設計だが、こちらは項目テキストにANSIコードが直接埋め込まれて
// いる(render.go 参照)ため、エスケープシーケンス自体は幅計算から除外しつつコードごと保持する点が異なる。
func truncateLine(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}

	runes := []rune(s)

	// 1周目: そもそも切り詰めが必要かどうかを判定するだけ(表示幅の合計を数える)。
	width := 0
	fits := true
	for i := 0; i < len(runes); {
		if j := skipAnsiEscape(runes, i); j != i {
			i = j
			continue
		}
		width += runewidth.RuneWidth(runes[i])
		if width > maxWidth {
			fits = false
			break
		}
		i++
	}
	if fits {
		return s
	}

	// 2周目: "..." の3文字分を引いた予算で先頭から組み立てる(ANSIコードはそのままコピー)。
	budget := maxWidth
	suffix := "..."
	if budget > 3 {
		budget -= 3
	} else {
		suffix = ""
	}

	var b strings.Builder
	width = 0
	for i := 0; i < len(runes); {
		if j := skipAnsiEscape(runes, i); j != i {
			b.WriteString(string(runes[i:j]))
			i = j
			continue
		}
		w := runewidth.RuneWidth(runes[i])
		if width+w > budget {
			break
		}
		b.WriteRune(runes[i])
		width += w
		i++
	}
	b.WriteString(suffix)
	// 途中で切ったことで開いたままになっている可能性のある色指定をリセットする
	// (色が使われていない行では単なる無害な追加バイトになる)。
	b.WriteString("\x1b[0m")
	return b.String()
}
