package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"x5ch-go/history"
	"x5ch-go/selector"
	"x5ch-go/transfer"
)

// manageQueue は転送待機列の一覧表示・削除画面。Ruby版 manage_queue に対応。
// 戻り値はCtrl+Cで中断されたかどうか(呼び出し元のselector.Runへ伝播させるため)。
func manageQueue(io *selector.IO, worker *transfer.Worker) bool {
	for {
		items := worker.GetQueueList()

		io.Print("\x1b[H\x1b[2J")
		io.Println("=== 転送待機列の管理 ===")
		io.Println(fmt.Sprintf(" 現在の待機数: %d", len(items)))
		io.Println(" 削除したいタスクの番号を入力してください。")
		io.Println(" (b: 戻る)")
		io.Println("------------------------------------------------")
		if len(items) == 0 {
			io.Println(" (待機中のタスクはありません)")
		} else {
			for i, item := range items {
				io.Println(fmt.Sprintf("[%d] %s", i, item.Title))
			}
		}
		io.Print("\r\nCommand > ")

		b, err := io.ReadKey()
		if err != nil {
			return false
		}
		if b == 0x03 {
			return true
		}
		if b == 'b' || b == 'q' {
			return false
		}
		io.Print(string(rune(b)))
		rest, err := io.ReadLine()
		if err != nil {
			if err == selector.ErrInterrupted {
				return true
			}
			return false
		}
		input := strings.TrimSpace(string(rune(b)) + rest)
		if idx, convErr := strconv.Atoi(input); convErr == nil {
			if deleted, ok := worker.DeleteAt(idx); ok {
				io.Println(fmt.Sprintf("\r\n\x1b[31m削除しました: %s\x1b[0m", deleted.Title))
				time.Sleep(1 * time.Second)
			}
		}
	}
}

// manageHistory は閲覧履歴の一覧表示・削除画面(メインメニューの'H'相当)。Ruby版 manage_history に対応。
func manageHistory(io *selector.IO, hist *history.Manager) bool {
	for {
		items := hist.GetRecentThreads()

		io.Print("\x1b[H\x1b[2J")
		io.Println("=== 閲覧履歴の管理 (削除) ===")
		io.Println(" 削除したい履歴の番号を入力してください。")
		io.Println(" (b: 戻る)")
		io.Println("------------------------------------------------")
		if len(items) == 0 {
			io.Println(" (履歴はありません)")
		} else {
			for i, item := range items {
				io.Println(fmt.Sprintf("[%d] %s (Read: %d)", i, item.Title, item.LastRead))
			}
		}
		io.Print("\r\nDelete No > ")

		b, err := io.ReadKey()
		if err != nil {
			return false
		}
		if b == 0x03 {
			return true
		}
		if b == 'b' || b == 'q' {
			return false
		}
		io.Print(string(rune(b)))
		rest, err := io.ReadLine()
		if err != nil {
			if err == selector.ErrInterrupted {
				return true
			}
			return false
		}
		input := strings.TrimSpace(string(rune(b)) + rest)
		idx, convErr := strconv.Atoi(input)
		if convErr != nil {
			continue
		}
		if idx >= 0 && idx < len(items) {
			target := items[idx]
			if hist.DeleteThread(target.BoardURL, target.DatFile) {
				io.Println(fmt.Sprintf("\r\n\x1b[31m履歴を削除しました: %s\x1b[0m", target.Title))
				time.Sleep(800 * time.Millisecond)
			}
		} else {
			io.Println("\r\n無効な番号です")
			time.Sleep(500 * time.Millisecond)
		}
	}
}
