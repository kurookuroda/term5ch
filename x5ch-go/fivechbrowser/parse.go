package fivechbrowser

import (
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"
)

var postChunkSplitPattern = regexp.MustCompile(
	`<div\s+[^>]*class=["'][^"']*clear post[^"']*["'][^>]*>`,
)

var (
	postIDPattern       = regexp.MustCompile(`<span\s+class="postid">(\d+)</span>`)
	postUsernamePattern = regexp.MustCompile(`(?s)<span\s+class="postusername">(.+?)</span>`)
	postDatePattern     = regexp.MustCompile(`<span\s+class="date">(.+?)</span>`)
	postUIDPattern      = regexp.MustCompile(`<span\s+class="uid">(.+?)</span>`)
	postContentPattern  = regexp.MustCompile(`(?s)<div\s+class="post-content">(.*?)</div>`)
	postContentFallback = regexp.MustCompile(`(?s)<div\s+class="post-content">(.*)`)
	trailingDivPattern  = regexp.MustCompile(`</div>\s*$`)
)

var hRestorePattern = regexp2.MustCompile(`(?<!h)(ttps?://)`, 0)

func ParsePosts(html string) []Post {
	chunks := postChunkSplitPattern.Split(html, -1)
	if len(chunks) > 0 {
		chunks = chunks[1:]
	}

	var posts []Post
	for _, chunk := range chunks {
		numMatch := postIDPattern.FindStringSubmatch(chunk)
		if numMatch == nil {
			continue
		}
		num := atoiSafe(numMatch[1])

		name := "名無し"
		if m := postUsernamePattern.FindStringSubmatch(chunk); m != nil {
			name = strings.TrimSpace(htmlTagPattern.ReplaceAllString(m[1], ""))
		}

		date := ""
		if m := postDatePattern.FindStringSubmatch(chunk); m != nil {
			date = strings.TrimSpace(m[1])
		}
		uid := ""
		if m := postUIDPattern.FindStringSubmatch(chunk); m != nil {
			uid = strings.TrimSpace(m[1])
		}
		date = date + " " + uid

		message := ""
		if m := postContentPattern.FindStringSubmatch(chunk); m != nil {
			message = m[1]
		} else if m := postContentFallback.FindStringSubmatch(chunk); m != nil {
			message = trailingDivPattern.ReplaceAllString(m[1], "")
		}

		rawMsg := message
		rawMsg = strings.ReplaceAll(rawMsg, "<br>", "\n")
		rawMsg = htmlTagPattern.ReplaceAllString(rawMsg, " ")
		rawMsg = strings.ReplaceAll(rawMsg, "&gt;", ">")
		rawMsg = strings.ReplaceAll(rawMsg, "&lt;", "<")
		rawMsg = strings.ReplaceAll(rawMsg, "&amp;", "&")
		rawMsg = strings.TrimSpace(rawMsg)

		cleanMessage, err := hRestorePattern.Replace(rawMsg, "h$1", -1, -1)
		if err != nil {
			cleanMessage = rawMsg
		}

		posts = append(posts, Post{
			Num:     num,
			Name:    name,
			Date:    date,
			Message: cleanMessage,
		})
	}

	return posts
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
