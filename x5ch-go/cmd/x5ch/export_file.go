package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"x5ch-go/fivechbrowser"
)

// filenameUnsafePattern はファイル名に使えない文字。
var filenameUnsafePattern = regexp.MustCompile(`[/\\:*?"<>|\r\n]`)

// exportOutputDir は `x5ch export`(TUI内エクスポート機能)の保存先ディレクトリ。
// X5CH_EXPORT_DIR未設定時は「x5chを実行したカレントディレクトリ」直下の x5ch_exports。
// (ホームディレクトリ基準にすると、Codespaces/Colab等で実行ディレクトリと
//
//	ホームディレクトリが別階層/別マウントになっている環境で見つけにくくなるため)
func exportOutputDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return envOr("X5CH_EXPORT_DIR", filepath.Join(cwd, "x5ch_exports"))
}

// sanitizeFilename はOSのファイル名として不正な文字を "_" に置換し、長すぎる場合は切り詰める。
func sanitizeFilename(s string) string {
	cleaned := strings.TrimSpace(filenameUnsafePattern.ReplaceAllString(s, "_"))
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	if cleaned == "" {
		return "no_title"
	}
	return cleaned
}

// exportFilenameBase は保存ファイル名(拡張子抜き)。dat_file(スレ立て時刻)+スレタイトルで構成し、
// 同じスレッドの複数回エクスポートでもファイルが一意に定まるようにする。
func exportFilenameBase(datFile, title string) string {
	datNum := strings.TrimSuffix(datFile, ".dat")
	return datNum + "_" + sanitizeFilename(title)
}

// performExport は指定スレッドを取得し、json または markdown いずれか1ファイルに書き出す。
// TUI(Pager/Selector双方)から呼ばれる共通処理。エラーは返さず、成否どちらも
// ユーザーにそのまま表示できるメッセージ文字列を返す。
func performExport(browser *fivechbrowser.Browser, boardURL, datFile string, asMarkdown bool) string {
	result, err := browser.ExportThreadData(boardURL, datFile, 0)
	if err != nil {
		if errors.Is(err, fivechbrowser.ErrThreadGone) {
			return "エクスポート失敗: スレッドはdat落ちしています"
		}
		if url := extractErrorURL(err); url != "" {
			return fmt.Sprintf("エクスポート失敗: %v (URL: %s)", err, url)
		}
		return fmt.Sprintf("エクスポート失敗: %v", err)
	}

	dir := exportOutputDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("エクスポート失敗: 保存先ディレクトリを作成できません (%v)", err)
	}

	base := exportFilenameBase(datFile, result.Thread.Title)
	ext := "json"
	body := ""
	if asMarkdown {
		ext = "md"
		body = result.ToMarkdown()
	} else {
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(result); err != nil {
			return fmt.Sprintf("エクスポート失敗: JSON変換エラー (%v)", err)
		}
		body = buf.String()
	}
	path := filepath.Join(dir, base+"."+ext)

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Sprintf("エクスポート失敗: 書き込みエラー (%v)", err)
	}

	return "エクスポートしました: " + path
}
