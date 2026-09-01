package main

import (
	"os"
	"path/filepath"
	"time"
)

// appConfig は Ruby版 config.rb (Config module) に対応する設定値。
// Rubyはコードを直接評価する設定ファイルだったが、Go版では環境変数 + ホームディレクトリの
// デフォルトパスを使うシンプルな方式にしている(意図的な設計変更)。
type appConfig struct {
	DiscordBotToken  string
	DiscordChannelID string
	HistoryFile      string
	QueueFile        string
	LockFile         string
	PIDFile          string
	CacheExpiration  time.Duration
	UserAgent        string
}

func loadConfig() appConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return appConfig{
		DiscordBotToken:  os.Getenv("X5CH_DISCORD_BOT_TOKEN"),
		DiscordChannelID: os.Getenv("X5CH_DISCORD_CHANNEL_ID"),
		HistoryFile:      envOr("X5CH_HISTORY_FILE", filepath.Join(home, ".x5ch_history.json")),
		QueueFile:        envOr("X5CH_QUEUE_FILE", filepath.Join(home, ".x5ch_queue.json")),
		LockFile:         envOr("X5CH_LOCK_FILE", filepath.Join(home, ".x5ch.lock")),
		PIDFile:          envOr("X5CH_PID_FILE", filepath.Join(home, ".x5ch.pid")),
		CacheExpiration:  300 * time.Second,
		UserAgent:        "w3m/0.5.3",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
