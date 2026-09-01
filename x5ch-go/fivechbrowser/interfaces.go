package fivechbrowser

// HistoryStore は閲覧履歴の永続化を担う。Ruby版の HistoryManager に対応するインターフェース。
// browser.go 側で具象実装(historymanager.HistoryManager等)を注入する想定。
type HistoryStore interface {
	GetLastRead(boardURL, datFile string) int
	Exists(boardURL, datFile string) bool
	AddNewThread(title, boardURL, datFile string)
}
