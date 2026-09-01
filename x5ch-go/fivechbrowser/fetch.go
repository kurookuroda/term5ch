package fivechbrowser

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	maxRedirects   = 5
	requestTimeout = 30 * time.Second
)

// Fetcher は5chへのHTTPアクセスを担当する
type Fetcher struct {
	client    *http.Client
	userAgent string
}

func NewFetcher(userAgent string) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("リダイレクト回数が上限に達しました")
				}
				return nil
			},
		},
		userAgent: userAgent,
	}
}

// Fetch はURLを取得し、本文(gzip展開済み)と最終URLを返す。
// gzip展開はnet/httpのTransportが自動で行うため、明示的なAccept-Encoding指定は不要。
func (f *Fetcher) Fetch(url string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("リクエスト作成エラー: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("通信エラー: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP Error: %d %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("本文読み取りエラー: %w", err)
	}

	finalURL := resp.Request.URL.String()
	return body, finalURL, nil
}
