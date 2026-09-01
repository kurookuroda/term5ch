package fivechbrowser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

const (
	menuURLJSON = "https://menu.5ch.io/bbsmenu.json"
	menuURLHTML = "https://menu.5ch.io/bbsmenu.html"
)

type bbsMenuJSON struct {
	MenuList []struct {
		CategoryName    string `json:"category_name"`
		CategoryContent []struct {
			BoardName string `json:"board_name"`
			URL       string `json:"url"`
		} `json:"category_content"`
	} `json:"menu_list"`
}

func GetMenu(f *Fetcher) ([]Category, error) {
	if cats, err := getMenuFromJSON(f); err == nil && len(cats) > 0 {
		return cats, nil
	}
	if cats, err := getMenuFromHTML(f); err == nil && len(cats) > 0 {
		return cats, nil
	}
	return nil, fmt.Errorf("メニューの取得に失敗しました(JSON/HTML両方とも失敗)")
}

func getMenuFromJSON(f *Fetcher) ([]Category, error) {
	body, _, err := f.Fetch(menuURLJSON)
	if err != nil {
		return nil, err
	}

	var data bbsMenuJSON
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("JSON解析エラー: %w", err)
	}

	var categories []Category
	for _, cat := range data.MenuList {
		var boards []Board
		for _, b := range cat.CategoryContent {
			if b.URL == "" {
				continue
			}
			url := normalizeMenuURL(b.URL)
			boards = append(boards, Board{Title: b.BoardName, URL: url})
		}
		if len(boards) > 0 {
			categories = append(categories, Category{Title: cat.CategoryName, Boards: boards})
		}
	}
	return categories, nil
}

var htmlMenuPattern = regexp.MustCompile(
	`(?i)(?:<B>([^<]+)</B>)|(?:<A HREF=["']?([^ >"']+)["']?[^>]*>([^<]+)</A>)`,
)

func getMenuFromHTML(f *Fetcher) ([]Category, error) {
	body, _, err := f.Fetch(menuURLHTML)
	if err != nil {
		return nil, err
	}

	html, err := decodeToUTF8(body)
	if err != nil {
		return nil, fmt.Errorf("エンコーディング変換エラー: %w", err)
	}

	var categories []Category
	var currentCategory string
	var currentBoards []Board
	hasCategory := false

	for _, m := range htmlMenuPattern.FindAllStringSubmatch(html, -1) {
		if m[1] != "" {
			if hasCategory && len(currentBoards) > 0 {
				categories = append(categories, Category{Title: currentCategory, Boards: currentBoards})
			}
			currentCategory = strings.TrimSpace(m[1])
			currentBoards = nil
			hasCategory = true
		} else if m[2] != "" {
			url := m[2]
			if !strings.Contains(url, "5ch.io") && !strings.Contains(url, "5ch.net") &&
				!strings.Contains(url, "2ch.net") && !strings.Contains(url, "bbspink.com") {
				continue
			}
			url = normalizeMenuURL(url)
			if hasCategory {
				currentBoards = append(currentBoards, Board{Title: strings.TrimSpace(m[3]), URL: url})
			}
		}
	}
	if hasCategory && len(currentBoards) > 0 {
		categories = append(categories, Category{Title: currentCategory, Boards: currentBoards})
	}

	return categories, nil
}

func normalizeMenuURL(url string) string {
	url = strings.ReplaceAll(url, "2ch.net", "5ch.io")
	url = strings.ReplaceAll(url, "5ch.net", "5ch.io")
	if strings.HasPrefix(url, "http:") {
		url = "https:" + strings.TrimPrefix(url, "http:")
	}
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return url
}

func decodeToUTF8(body []byte) (string, error) {
	decoded, err := japanese.ShiftJIS.NewDecoder().Bytes(body)
	if err != nil {
		return string(body), nil
	}
	return string(decoded), nil
}
