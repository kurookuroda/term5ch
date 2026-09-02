package fivechbrowser

import (
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExportPost はアーカイブ用途の完全な投稿データ。TUI/Discord/TTS用の Post とは別に、
// 原文保持(body_raw)・派生表示(body_display)・返信関係・mail欄などを持つ。
type ExportPost struct {
	ExternalID        string `json:"external_id"`
	Num               int    `json:"num"`
	AuthorNameDisplay string `json:"author_name_display"`
	MailEncoded       string `json:"mail_encoded,omitempty"`
	MailDecoded       string `json:"mail_decoded,omitempty"`
	UserID            string `json:"user_id"`
	PostedAt          string `json:"posted_at,omitempty"`
	PostedAtRaw       string `json:"posted_at_raw"`
	ReplyTo           []int  `json:"reply_to,omitempty"`
	BodyRaw           string `json:"body_raw"`
	BodyDisplay       string `json:"body_display"`
	BodyHTMLOriginal  string `json:"body_html_original"`
}

// ExportThread はアーカイブ用途のスレッドメタデータ。
type ExportThread struct {
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	BoardName  string `json:"board_name,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	PostCount  int    `json:"post_count"`
}

// ExportSource はスクレイピング元の情報。
type ExportSource struct {
	Provider  string `json:"provider"`
	BoardURL  string `json:"board_url"`
	DatFile   string `json:"dat_file"`
	ThreadURL string `json:"thread_url"`
	ScrapedAt string `json:"scraped_at"`
}

// ExportResult は export サブコマンドが出力するJSON全体の構造。
type ExportResult struct {
	Source ExportSource `json:"source"`
	Thread ExportThread `json:"thread"`
	Posts  []ExportPost `json:"posts"`
}

var (
	// mailLinkPattern はCloudflareのメール難読化リンクを検出する。
	// マッチしない場合はmail欄が空(または通常のリンクではない形)であることを示す。
	mailLinkPattern = regexp.MustCompile(`<a\s+[^>]*href="/cdn-cgi/l/email-protection#([0-9a-fA-F]+)"[^>]*>(.*?)</a>`)

	replyLinkTagPattern = regexp.MustCompile(`<a[^>]*class="reply_link"[^>]*>`)
	hrefNumPattern       = regexp.MustCompile(`href="[^"]*?/(\d+)"`)

	postedAtPattern = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2})\([月火水木金土日]\)\s+(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?`)
)

// decodeCFEmail はCloudflareのメール難読化(cdn-cgi/l/email-protection)をデコードする。
// アルゴリズムはCloudflare公式仕様(先頭2桁がXORキー、残りを2桁ずつXOR)に基づく。
func decodeCFEmail(hexStr string) (string, error) {
	if len(hexStr) < 2 || len(hexStr)%2 != 0 {
		return "", fmt.Errorf("不正な長さ: %s", hexStr)
	}
	key, err := strconv.ParseUint(hexStr[0:2], 16, 8)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for i := 2; i < len(hexStr); i += 2 {
		b, err := strconv.ParseUint(hexStr[i:i+2], 16, 8)
		if err != nil {
			return "", err
		}
		sb.WriteByte(byte(b) ^ byte(key))
	}
	return sb.String(), nil
}

// extractReplyTo は本文HTML中の class="reply_link" アンカーから返信先レス番号を抽出する。
// 本文中の引用符("&gt;...")など reply_link クラスを持たないものは対象外。
func extractReplyTo(contentHTML string) []int {
	var result []int
	for _, tag := range replyLinkTagPattern.FindAllString(contentHTML, -1) {
		if m := hrefNumPattern.FindStringSubmatch(tag); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				result = append(result, n)
			}
		}
	}
	return result
}

// parsePostedAt は "2025/12/16(火) 05:05:09.95" 形式をISO8601(JST)に変換する。
// パースできない場合は空文字を返す(呼び出し側は posted_at_raw を代わりに使う)。
func parsePostedAt(raw string) string {
	m := postedAtPattern.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	hour, _ := strconv.Atoi(m[4])
	minute, _ := strconv.Atoi(m[5])
	second, _ := strconv.Atoi(m[6])

	nsec := 0
	if m[7] != "" {
		frac := m[7]
		for len(frac) < 9 {
			frac += "0"
		}
		nsec, _ = strconv.Atoi(frac[:9])
	}

	jst := time.FixedZone("JST", 9*3600)
	t := time.Date(year, time.Month(month), day, hour, minute, second, nsec, jst)
	return t.Format(time.RFC3339Nano)
}

// extractPlainText はタグを除去し、標準ライブラリでHTMLエンティティを正しくデコードする。
// Ruby版由来の "&gt;/&lt;/&amp;のみ手動置換" バグを修正したもの(h補完は行わない=原文のまま)。
func extractPlainText(innerHTML string) string {
	text := strings.ReplaceAll(innerHTML, "<br>", "\n")
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = htmlpkg.UnescapeString(text)
	return strings.TrimSpace(text)
}

// ParsePostsForExport はスレッドHTMLをアーカイブ用の完全な構造でパースする。
// threadExternalID は "5ch:host/path/datnum" 形式で、各投稿の external_id 組み立てに使う。
// Ruby版には存在しない、このプロジェクト独自の機能。
func ParsePostsForExport(html string, threadExternalID string) []ExportPost {
	chunks := postChunkSplitPattern.Split(html, -1)
	if len(chunks) > 0 {
		chunks = chunks[1:]
	}

	var posts []ExportPost
	for _, chunk := range chunks {
		numMatch := postIDPattern.FindStringSubmatch(chunk)
		if numMatch == nil {
			continue
		}
		num := atoiSafe(numMatch[1])

		authorDisplay := "名無し"
		mailEncoded := ""
		mailDecoded := ""
		if m := postUsernamePattern.FindStringSubmatch(chunk); m != nil {
			rawName := m[1]
			if mm := mailLinkPattern.FindStringSubmatch(rawName); mm != nil {
				mailEncoded = mm[1]
				authorDisplay = strings.TrimSpace(htmlTagPattern.ReplaceAllString(mm[2], ""))
				if decoded, err := decodeCFEmail(mailEncoded); err == nil {
					mailDecoded = decoded
				}
			} else {
				authorDisplay = strings.TrimSpace(htmlTagPattern.ReplaceAllString(rawName, ""))
			}
		}

		postedAtRaw := ""
		if m := postDatePattern.FindStringSubmatch(chunk); m != nil {
			postedAtRaw = strings.TrimSpace(m[1])
		}
		userID := ""
		if m := postUIDPattern.FindStringSubmatch(chunk); m != nil {
			userID = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[1]), "ID:"))
		}

		bodyHTML := ""
		if m := postContentPattern.FindStringSubmatch(chunk); m != nil {
			bodyHTML = m[1]
		} else if m := postContentFallback.FindStringSubmatch(chunk); m != nil {
			bodyHTML = trailingDivPattern.ReplaceAllString(m[1], "")
		}

		replyTo := extractReplyTo(bodyHTML)
		bodyRaw := extractPlainText(bodyHTML)
		bodyDisplay, err := hRestorePattern.Replace(bodyRaw, "h$1", -1, -1)
		if err != nil {
			bodyDisplay = bodyRaw
		}

		posts = append(posts, ExportPost{
			ExternalID:        fmt.Sprintf("%s#%d", threadExternalID, num),
			Num:               num,
			AuthorNameDisplay: authorDisplay,
			MailEncoded:       mailEncoded,
			MailDecoded:       mailDecoded,
			UserID:            userID,
			PostedAt:          parsePostedAt(postedAtRaw),
			PostedAtRaw:       postedAtRaw,
			ReplyTo:           replyTo,
			BodyRaw:           bodyRaw,
			BodyDisplay:       bodyDisplay,
			BodyHTMLOriginal:  strings.TrimSpace(bodyHTML),
		})
	}
	return posts
}

// breadcrumbLD はJSON-LD構造化データ(BreadcrumbList)の最小限のデコード用構造体。
type breadcrumbLD struct {
	Type            string `json:"@type"`
	ItemListElement []struct {
		Position int    `json:"position"`
		Name     string `json:"name"`
	} `json:"itemListElement"`
}

var ldJSONPattern = regexp.MustCompile(`(?s)<script\s+type="application/ld\+json">(.*?)</script>`)

// extractBoardName はページ全体のHTMLからJSON-LDパンくずリストの板名(position=2)を抽出する。
// 抽出できなければ空文字を返す(1件のサンプルで確認した構造に基づく、実データが限定的な点に注意)。
func extractBoardName(fullHTML string) string {
	m := ldJSONPattern.FindStringSubmatch(fullHTML)
	if m == nil {
		return ""
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal([]byte(m[1]), &rawItems); err != nil {
		return ""
	}

	for _, raw := range rawItems {
		var doc breadcrumbLD
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		if doc.Type != "BreadcrumbList" {
			continue
		}
		for _, item := range doc.ItemListElement {
			if item.Position == 2 {
				return item.Name
			}
		}
	}
	return ""
}

// datTimestampToRFC3339 は dat ファイル名(スレ立て時刻のUNIXタイムスタンプ)をISO8601(UTC)に変換する。
func datTimestampToRFC3339(datFile string) string {
	tsStr := strings.TrimSuffix(datFile, ".dat")
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}
