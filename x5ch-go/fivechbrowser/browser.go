package fivechbrowser

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrThreadGone = errors.New("スレッドはdat落ちしています")

type cachedThreads struct {
	data []ThreadInfo
	time time.Time
}

type Browser struct {
	fetcher     *Fetcher
	history     HistoryStore
	cacheExpire time.Duration

	menuMu    sync.Mutex
	menuCache []Category

	threadMu    sync.Mutex
	threadCache map[string]cachedThreads
}

func NewBrowser(userAgent string, history HistoryStore, cacheExpire time.Duration) *Browser {
	return &Browser{
		fetcher:     NewFetcher(userAgent),
		history:     history,
		cacheExpire: cacheExpire,
		threadCache: make(map[string]cachedThreads),
	}
}

func (b *Browser) GetMenu() ([]Category, error) {
	b.menuMu.Lock()
	defer b.menuMu.Unlock()

	if b.menuCache != nil {
		return b.menuCache, nil
	}

	cats, err := GetMenu(b.fetcher)
	if err != nil {
		return nil, err
	}
	b.menuCache = cats
	return cats, nil
}

func (b *Browser) GetThreads(board *Board, forceReload bool) ([]ThreadInfo, error) {
	b.threadMu.Lock()
	if !forceReload {
		if cached, ok := b.threadCache[board.URL]; ok && time.Since(cached.time) < b.cacheExpire {
			b.threadMu.Unlock()
			return cached.data, nil
		}
	}
	staleCache, hasStale := b.threadCache[board.URL]
	b.threadMu.Unlock()

	threads, err := GetThreads(b.fetcher, b.history, board)
	if err != nil {
		if hasStale {
			return staleCache.data, nil
		}
		return nil, err
	}

	b.threadMu.Lock()
	b.threadCache[board.URL] = cachedThreads{data: threads, time: time.Now()}
	b.threadMu.Unlock()

	return threads, nil
}

func (b *Browser) SearchGlobal(keyword string) ([]ThreadInfo, error) {
	return SearchGlobal(b.fetcher, b.history, keyword)
}

func (b *Browser) GetThreadData(t *ThreadInfo) ([]Post, error) {
	readURL, err := buildReadURL(t.BoardURL, t.DatFile)
	if err != nil {
		return nil, err
	}

	body, finalURL, err := b.fetcher.Fetch(readURL)
	if err != nil {
		return nil, fmt.Errorf("スレッド取得に失敗しました: %w", err)
	}

	html, err := decodeToUTF8(body)
	if err != nil {
		return nil, fmt.Errorf("エンコーディング変換エラー: %w", err)
	}

	if strings.Contains(html, "dat落ち") {
		return nil, ErrThreadGone
	}

	posts := ParsePosts(html)
	t.URL = finalURL

	DetectAndAddNextThread(b.fetcher, b.history, posts, *t)

	return posts, nil
}

func buildReadURL(boardURL, datFile string) (string, error) {
	u, err := url.Parse(boardURL)
	if err != nil {
		return "", fmt.Errorf("board_urlの解析に失敗: %w", err)
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) == 0 || segments[len(segments)-1] == "" {
		return "", fmt.Errorf("board_urlから板名を特定できません: %s", boardURL)
	}
	boardName := segments[len(segments)-1]

	datNum := strings.TrimSuffix(datFile, ".dat")
	if _, err := strconv.Atoi(datNum); err != nil {
		return "", fmt.Errorf("不正なdat_file: %s", datFile)
	}

	return u.Scheme + "://" + u.Host + "/test/read.cgi/" + boardName + "/" + datNum + "/", nil
}
