package fivechbrowser

import (
	"net/url"
	"regexp"
	"strings"
)

var currentBoardURLPattern = regexp.MustCompile(`^https?://([^/]+)/([^/]+)/`)
var titleTagPattern = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
var titleSuffixPattern = regexp.MustCompile(`(?is)\s*[-|]\s*5ch\.(net|io).*`)

func DetectAndAddNextThread(f *Fetcher, history HistoryStore, posts []Post, current ThreadInfo) {
	var candidates []Post
	for _, p := range posts {
		if p.Num >= 900 {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return
	}

	m := currentBoardURLPattern.FindStringSubmatch(current.BoardURL)
	if m == nil {
		return
	}
	server := m[1]
	board := m[2]

	nextThreadPattern := regexp.MustCompile(
		`https?://` + regexp.QuoteMeta(server) + `/test/read\.cgi/` + regexp.QuoteMeta(board) + `/(\d+)/?`,
	)

	for _, post := range candidates {
		for _, match := range nextThreadPattern.FindAllStringSubmatch(post.Message, -1) {
			datKey := match[1]
			datFile := datKey + ".dat"

			if history.Exists(current.BoardURL, datFile) {
				continue
			}
			if datFile == current.DatFile {
				continue
			}

			title := fetchThreadTitle(f, current.BoardURL, datKey)
			if title != "" {
				history.AddNewThread(title, current.BoardURL, datFile)
			}
		}
	}
}

func fetchThreadTitle(f *Fetcher, boardURL, datKey string) string {
	u, err := url.Parse(boardURL)
	if err != nil {
		return ""
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) == 0 || segments[len(segments)-1] == "" {
		return ""
	}
	boardName := segments[len(segments)-1]

	readURL := u.Scheme + "://" + u.Host + "/test/read.cgi/" + boardName + "/" + datKey + "/"

	body, _, err := f.Fetch(readURL)
	if err != nil {
		return ""
	}

	html, err := decodeToUTF8(body)
	if err != nil {
		return ""
	}

	tm := titleTagPattern.FindStringSubmatch(html)
	if tm == nil {
		return ""
	}

	title := strings.TrimSpace(tm[1])
	title = titleSuffixPattern.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}
