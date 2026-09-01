package history

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"x5ch-go/fivechbrowser"
)

type entry struct {
	Res             int    `json:"res"`
	Title           string `json:"title"`
	BoardURL        string `json:"board_url"`
	DatFile         string `json:"dat_file"`
	Timestamp       int64  `json:"timestamp"`
	DiscordThreadID string `json:"discord_thread_id,omitempty"`
}

type Manager struct {
	mu       sync.Mutex
	data     map[string]entry
	filePath string
}

func NewManager(filePath string) (*Manager, error) {
	m := &Manager{
		data:     make(map[string]entry),
		filePath: filePath,
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) load() error {
	body, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var data map[string]entry
	if err := json.Unmarshal(body, &data); err != nil {
		return nil
	}
	m.data = data
	return nil
}

func (m *Manager) save() error {
	body, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath, body, 0644)
}

func normalizeURL(rawURL string) string {
	u := rawURL
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	u = strings.TrimSuffix(u, "/")
	u = strings.ReplaceAll(u, "2ch.net", "5ch.io")
	u = strings.ReplaceAll(u, "5ch.net", "5ch.io")
	return u
}

func GenerateKey(boardURL, datFile string) string {
	return normalizeURL(boardURL) + "::" + datFile
}

func (m *Manager) GetLastRead(boardURL, datFile string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[GenerateKey(boardURL, datFile)].Res
}

func (m *Manager) GetDiscordThreadID(boardURL, datFile string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[GenerateKey(boardURL, datFile)].DiscordThreadID
}

func (m *Manager) Exists(boardURL, datFile string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[GenerateKey(boardURL, datFile)]
	return ok
}

func (m *Manager) HasHistoryInBoard(boardURL string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	target := normalizeURL(boardURL)
	for _, e := range m.data {
		if normalizeURL(e.BoardURL) == target {
			return true
		}
	}
	return false
}

func (m *Manager) HasHistoryInCategory(boards []fivechbrowser.Board) bool {
	for _, b := range boards {
		if m.HasHistoryInBoard(b.URL) {
			return true
		}
	}
	return false
}

func (m *Manager) AddNewThread(title, boardURL, datFile string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := GenerateKey(boardURL, datFile)
	if _, exists := m.data[key]; exists {
		return
	}
	m.data[key] = entry{
		Res:       0,
		Title:     title,
		BoardURL:  boardURL,
		DatFile:   datFile,
		Timestamp: time.Now().Unix(),
	}
	_ = m.save()
}

func (m *Manager) DeleteThread(boardURL, datFile string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := GenerateKey(boardURL, datFile)
	if _, exists := m.data[key]; !exists {
		return false
	}
	delete(m.data, key)
	_ = m.save()
	return true
}

func (m *Manager) UpdateHistory(t fivechbrowser.ThreadInfo, resNum int, discordThreadID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := GenerateKey(t.BoardURL, t.DatFile)
	current := m.data[key]

	newRes := current.Res
	if resNum > newRes {
		newRes = resNum
	}

	newDiscordID := current.DiscordThreadID
	if discordThreadID != "" {
		newDiscordID = discordThreadID
	}

	m.data[key] = entry{
		Res:             newRes,
		Title:           t.Title,
		BoardURL:        t.BoardURL,
		DatFile:         t.DatFile,
		Timestamp:       time.Now().Unix(),
		DiscordThreadID: newDiscordID,
	}
	_ = m.save()
}

// recentThreadEntry はソート用にタイムスタンプを保持したまま返すための内部型。
type RecentThread struct {
	fivechbrowser.ThreadInfo
	Timestamp int64
}

// GetRecentThreads は履歴を新しい順に返す。
func (m *Manager) GetRecentThreads() []RecentThread {
	m.mu.Lock()
	defer m.mu.Unlock()

	threads := make([]RecentThread, 0, len(m.data))
	for _, e := range m.data {
		threads = append(threads, RecentThread{
			ThreadInfo: fivechbrowser.ThreadInfo{
				Title:    e.Title,
				BoardURL: e.BoardURL,
				DatFile:  e.DatFile,
				LastRead: e.Res,
			},
			Timestamp: e.Timestamp,
		})
	}
	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i].Timestamp > threads[j].Timestamp
	})
	return threads
}

// コンパイル時にHistoryStoreインターフェースを満たすことを保証する。
var _ fivechbrowser.HistoryStore = (*Manager)(nil)
