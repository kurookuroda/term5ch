package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	"x5ch-go/fivechbrowser"
	"x5ch-go/history"
	"x5ch-go/keyreader"
	"x5ch-go/pager"
)

// showThread は1スレッドを取得してViPager相当で表示し、既読位置を履歴に保存する。
// Ruby版 FiveChBrowser#show_thread に対応。
func showThread(browser *fivechbrowser.Browser, hist *history.Manager, t fivechbrowser.ThreadInfo, kr *keyreader.KeyReader, out io.Writer, fd int) {
	if saved := hist.GetLastRead(t.BoardURL, t.DatFile); saved > 0 {
		t.LastRead = saved
	}

	fmt.Fprintln(out, "スレッド取得中...")
	posts, err := browser.GetThreadData(&t)

	if err != nil || len(posts) == 0 {
		fmt.Fprintln(out, fetchFailureMessage(err))
		waitForKey(kr)
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

	result, err := pager.New(content).Start(kr, out, fd)
	if err == nil && result != nil && result.Res > 0 {
		hist.UpdateHistory(t, result.Res, "")
		fmt.Fprintf(out, "\r\n履歴を更新しました: %d\r\n", result.Res)
		time.Sleep(500 * time.Millisecond)
	}
}

// fetchFailureMessage はGetThreadDataの失敗理由を人間が読める形にする。
// err==nilでレス0件の場合(fetch自体は成功したがparse_postsが1件も一致しなかった)を、
// dat落ちや通信エラーと区別する。Crystal版のBrowserError.url活用と同じ設計。
func fetchFailureMessage(err error) string {
	switch {
	case err == nil:
		return "取得はできましたが、レスを1件も抽出できませんでした(パース不一致の可能性)"
	case errors.Is(err, fivechbrowser.ErrThreadGone):
		return "dat落ちしています"
	default:
		var browserErr *fivechbrowser.BrowserError
		if errors.As(err, &browserErr) && browserErr.URL != "" {
			return fmt.Sprintf("通信エラー: %s\r\nURL: %s", browserErr.Msg, browserErr.URL)
		}
		return fmt.Sprintf("予期しないエラー: %v", err)
	}
}

// showRecentStream は複数スレッドの新着をまとめて表示する。
// Ruby版 FiveChBrowser#show_recent_stream に対応。
func showRecentStream(browser *fivechbrowser.Browser, hist *history.Manager, threads []history.RecentThread, kr *keyreader.KeyReader, out io.Writer, fd int) {
	var content []pager.ContentItem

	for idx := range threads {
		th := threads[idx].ThreadInfo
		fmt.Fprintf(out, "\x1b[2K\r(%d/%d) 取得中: %s", idx+1, len(threads), th.Title)

		posts, err := browser.GetThreadData(&th)
		if err != nil {
			content = append(content, pager.ContentItem{Type: pager.ContentError, Thread: &th, Message: fetchFailureMessage(err) + " (" + th.Title + ")"})
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

	result, err := pager.New(content).Start(kr, out, fd)
	if err == nil && result != nil && result.Thread != nil && result.Res > 0 {
		hist.UpdateHistory(*result.Thread, result.Res, "")
		fmt.Fprintf(out, "\r\n履歴を更新しました: %s (%d)\r\n", result.Thread.Title, result.Res)
		time.Sleep(500 * time.Millisecond)
	}
}

func waitForKey(kr *keyreader.KeyReader) {
	_, _ = kr.ReadByte()
}
