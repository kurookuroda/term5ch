package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"x5ch-go/fivechbrowser"
	"x5ch-go/transfer"
)

// apiBase はDiscord APIのベースURL。テストからローカルサーバーに差し替えられるよう変数にしている。
var apiBase = "https://discord.com/api/v10"

// testAPIBaseOverride はテストコードから apiBase を差し替えるためのフック。
var testAPIBaseOverride string

func currentAPIBase() string {
	if testAPIBaseOverride != "" {
		return testAPIBaseOverride
	}
	return apiBase
}

type Manager struct {
	token     string
	channelID string
	client    *http.Client
}

func NewManager(token, channelID string) *Manager {
	return &Manager{
		token:     token,
		channelID: channelID,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *Manager) Enabled() bool {
	if m.token == "" || strings.Contains(m.token, "YOUR_BOT_TOKEN") {
		return false
	}
	if m.channelID == "" {
		return false
	}
	return true
}

func (m *Manager) CreateThread(title string) (string, error) {
	if !m.Enabled() {
		return "", fmt.Errorf("Discord機能が無効です(トークン/チャンネルID未設定)")
	}

	safeTitle := truncateRunes(title, 95)

	body, err := json.Marshal(map[string]interface{}{
		"name":                  safeTitle,
		"type":                  11,
		"auto_archive_duration": 1440,
	})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/channels/%s/threads", currentAPIBase(), m.channelID)
	resp, err := m.doPost(url, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", &transfer.DiscordAPIError{Msg: fmt.Sprintf("%d %s", resp.StatusCode, string(respBody))}
	}

	var data struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("レスポンス解析エラー: %w", err)
	}
	return data.ID, nil
}

func (m *Manager) SendMessage(threadID string, post fivechbrowser.Post) error {
	if !m.Enabled() || threadID == "" {
		return nil
	}

	header := fmt.Sprintf("**%d** : %s : %s", post.Num, post.Name, post.Date)
	fullContent := header + "\n" + post.Message

	if runeLen(fullContent) <= 2000 {
		return m.postContent(threadID, fullContent)
	}

	parts := splitByRunes(fullContent, 1900)
	for i, part := range parts {
		content := part
		if i < len(parts)-1 {
			content += "\n(続く...)"
		}
		if err := m.postContent(threadID, content); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (m *Manager) postContent(threadID, content string) error {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/channels/%s/messages", currentAPIBase(), threadID)

	for {
		resp, err := m.doPost(url, body)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp)
			resp.Body.Close()
			time.Sleep(retryAfter)
			continue
		}

		if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return &transfer.DiscordAPIError{Msg: fmt.Sprintf("%d %s", resp.StatusCode, string(respBody))}
		}

		resp.Body.Close()
		return nil
	}
}

func parseRetryAfter(resp *http.Response) time.Duration {
	var data struct {
		RetryAfter float64 `json:"retry_after"`
	}
	body, err := io.ReadAll(resp.Body)
	if err == nil {
		_ = json.Unmarshal(body, &data)
	}
	if data.RetryAfter <= 0 {
		return 1 * time.Second
	}
	return time.Duration(data.RetryAfter * float64(time.Second))
}

func (m *Manager) doPost(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+m.token)
	req.Header.Set("Content-Type", "application/json")

	return m.client.Do(req)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func runeLen(s string) int {
	return len([]rune(s))
}

func splitByRunes(s string, n int) []string {
	r := []rune(s)
	var chunks []string
	for i := 0; i < len(r); i += n {
		end := i + n
		if end > len(r) {
			end = len(r)
		}
		chunks = append(chunks, string(r[i:end]))
	}
	return chunks
}

var _ transfer.DiscordClient = (*Manager)(nil)
