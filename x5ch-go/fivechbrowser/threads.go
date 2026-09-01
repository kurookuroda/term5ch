package fivechbrowser

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var subjectLinePattern = regexp.MustCompile(`^(\d+\.dat)<>(.*?)\((\d+)\)\s*$`)

func GetThreads(f *Fetcher, history HistoryStore, board *Board) ([]ThreadInfo, error) {
	subjectURL := board.URL + "subject.txt"

	body, finalURL, err := f.Fetch(subjectURL)
	if err != nil {
		return nil, fmt.Errorf("スレッド一覧の取得に失敗しました: %w", err)
	}

	if finalURL != subjectURL {
		board.URL = strings.TrimSuffix(finalURL, "subject.txt")
	}

	data, err := decodeToUTF8(body)
	if err != nil {
		return nil, fmt.Errorf("エンコーディング変換エラー: %w", err)
	}

	now := time.Now().Unix()
	var threads []ThreadInfo

	for _, line := range strings.Split(data, "\n") {
		m := subjectLinePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		datFile := m[1]
		title := strings.TrimSpace(m[2])
		count, err := strconv.Atoi(m[3])
		if err != nil {
			continue
		}

		ikioi := calcIkioi(datFile, count, now)
		lastRead := history.GetLastRead(board.URL, datFile)

		threads = append(threads, ThreadInfo{
			DatFile:  datFile,
			Title:    title,
			Count:    count,
			Ikioi:    ikioi,
			BoardURL: board.URL,
			LastRead: lastRead,
		})
	}

	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i].Ikioi > threads[j].Ikioi
	})

	return threads, nil
}

func calcIkioi(datFile string, count int, now int64) float64 {
	tsStr := strings.TrimSuffix(datFile, ".dat")
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return 0
	}

	elapsedSeconds := now - ts
	if elapsedSeconds < 1 {
		elapsedSeconds = 1
	}
	elapsedDays := float64(elapsedSeconds) / 86400.0

	return float64(count) / elapsedDays
}
