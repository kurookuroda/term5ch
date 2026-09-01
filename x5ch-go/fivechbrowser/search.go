package fivechbrowser

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const searchBaseURL = "https://ff5ch.syoboi.jp/?q="

var searchResultPattern = regexp.MustCompile(
	`(?i)<a\s+[^>]*href="(https?://[^.]+\.5ch\.(?:net|io)/test/read\.cgi/[^/]+/\d+/?)"[^>]*>(.+?)</a>`,
)

var threadURLPattern = regexp.MustCompile(
	`https?://([^.]+)\.5ch\.(?:net|io)/test/read\.cgi/([^/]+)/(\d+)/?`,
)

var titleCountPattern = regexp.MustCompile(`^(.*)\((\d+)\)$`)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func SearchGlobal(f *Fetcher, history HistoryStore, keyword string) ([]ThreadInfo, error) {
	searchURL := searchBaseURL + url.QueryEscape(keyword)

	body, _, err := f.Fetch(searchURL)
	if err != nil {
		return nil, fmt.Errorf("検索エラー: %w", err)
	}

	html := toValidUTF8(body)

	var results []ThreadInfo
	for _, m := range searchResultPattern.FindAllStringSubmatch(html, -1) {
		fullURL := m[1]
		rawTitle := m[2]

		urlParts := threadURLPattern.FindStringSubmatch(fullURL)
		if urlParts == nil {
			continue
		}
		server := urlParts[1]
		boardName := urlParts[2]
		datNum := urlParts[3]

		title := strings.TrimSpace(htmlTagPattern.ReplaceAllString(rawTitle, ""))
		count := 0
		if cm := titleCountPattern.FindStringSubmatch(title); cm != nil {
			title = strings.TrimSpace(cm[1])
			if n, err := strconv.Atoi(cm[2]); err == nil {
				count = n
			}
		}

		boardURL := fmt.Sprintf("https://%s.5ch.io/%s/", server, boardName)
		datFile := datNum + ".dat"
		lastRead := history.GetLastRead(boardURL, datFile)

		results = append(results, ThreadInfo{
			Title:    title,
			Count:    count,
			Ikioi:    0,
			BoardURL: boardURL,
			DatFile:  datFile,
			URL:      fmt.Sprintf("https://%s.5ch.io/test/read.cgi/%s/%s/", server, boardName, datNum),
			LastRead: lastRead,
		})
	}

	return results, nil
}

func toValidUTF8(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	return strings.ToValidUTF8(string(body), "\uFFFD")
}
