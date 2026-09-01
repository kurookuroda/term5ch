package main

import (
	"fmt"
	"io"
	"time"

	"x5ch-go/fivechbrowser"
	"x5ch-go/history"
	"x5ch-go/pager"
)

// showThread は1スレッドを取得してViPager相当で表示し、既読位置を履歴に保存する。
// Ruby版 FiveChBrowser#show_thread に対応。
func showThread(browser *fivechbrowser.Browser, hist *history.Manager, t fivechbrowser.ThreadInfo, in io.Reader, out io.Writer, fd int) {
	if saved := hist.GetLastRead(t.BoardURL, t.DatFile); saved > 0 {
		t.LastRead = saved
	}

	fmt.Fprintln(out, "スレッド取得中...")
	posts, err := browser.GetThreadData(&t)
	if err != nil || len(posts) == 0 {
		fmt.Fprintln(out, "取得失敗またはdat落ち")
		waitForKey(in)
		return
	}
	t.Count = len(posts)

	content := []pager.ContentItem{{Type: pager.ContentHeader, Thread: &t}}
	markerInserted := false
	for i := range posts {
		p := posts[i]
		if !markerInserted && p.Num > t.LastRead {
			content = append(content, pager.ContentItem{Type: pager.ContentUnreadMarker, Thread: &t})
			markerInserted = true
		}
		content = append(content, pager.ContentItem{Type: pager.ContentPost, Thread: &t, Post: &p})
	}
	if !markerInserted {
		content = append(content, pager.ContentItem{Type: pager.ContentUnreadMarker, Thread: &t})
		content = append(content, pager.ContentItem{Type: pager.ContentSystemMsg, Thread: &t, Message: "(新着なし - 最終レスまで既読です)"})
	}

	result, err := pager.New(content).Start(in, out, fd)
	if err == nil && result != nil && result.Res > 0 {
		hist.UpdateHistory(t, result.Res, "")
		fmt.Fprintf(out, "\r\n履歴を更新しました: %d\r\n", result.Res)
		time.Sleep(500 * time.Millisecond)
	}
}

// showRecentStream は複数スレッドの新着をまとめて表示する。
// Ruby版 FiveChBrowser#show_recent_stream に対応。
func showRecentStream(browser *fivechbrowser.Browser, hist *history.Manager, threads []history.RecentThread, in io.Reader, out io.Writer, fd int) {
	var content []pager.ContentItem

	for idx := range threads {
		th := threads[idx].ThreadInfo
		fmt.Fprintf(out, "\x1b[2K\r(%d/%d) 取得中: %s", idx+1, len(threads), th.Title)

		posts, err := browser.GetThreadData(&th)
		if err != nil {
			content = append(content, pager.ContentItem{Type: pager.ContentError, Thread: &th, Message: "取得失敗: " + th.Title})
			continue
		}
		th.Count = len(posts)
		lastRead := th.LastRead

		content = append(content, pager.ContentItem{Type: pager.ContentHeader, Thread: &th})

		var newPosts []fivechbrowser.Post
		for _, p := range posts {
			if p.Num > lastRead {
				newPosts = append(newPosts, p)
			}
		}

		switch {
		case len(newPosts) == 0 && lastRead == 0:
			content = append(content, pager.ContentItem{Type: pager.ContentUnreadMarker, Thread: &th})
			for i := range posts {
				p := posts[i]
				content = append(content, pager.ContentItem{Type: pager.ContentPost, Thread: &th, Post: &p})
			}
		case len(newPosts) == 0:
			content = append(content, pager.ContentItem{Type: pager.ContentSystemMsg, Thread: &th, Message: "(新着なし)"})
		default:
			if lastRead > 0 {
				content = append(content, pager.ContentItem{Type: pager.ContentUnreadMarker, Thread: &th})
			}
			for i := range newPosts {
				p := newPosts[i]
				content = append(content, pager.ContentItem{Type: pager.ContentPost, Thread: &th, Post: &p})
			}
		}
		content = append(content, pager.ContentItem{Type: pager.ContentSeparator, Thread: &th})
	}

	if len(content) == 0 {
		fmt.Fprintln(out, "\r\n表示できる内容がありません")
		time.Sleep(1 * time.Second)
		return
	}

	result, err := pager.New(content).Start(in, out, fd)
	if err == nil && result != nil && result.Thread != nil && result.Res > 0 {
		hist.UpdateHistory(*result.Thread, result.Res, "")
		fmt.Fprintf(out, "\r\n履歴を更新しました: %s (%d)\r\n", result.Thread.Title, result.Res)
		time.Sleep(500 * time.Millisecond)
	}
}

func waitForKey(in io.Reader) {
	buf := make([]byte, 1)
	_, _ = in.Read(buf)
}
