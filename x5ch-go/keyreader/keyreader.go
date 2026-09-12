// Package keyreader は標準入力を読み続ける唯一のバックグラウンドgoroutineを提供する。
//
// 背景: selector.Run/pager.Startが呼ばれるたびに専用の読み取りgoroutineを
// 使い捨てで生成していたが、関数が終了してもそのgoroutineは実際のブロッキング
// read呼び出し中であるため止める手段が無く、生き残り続けていた。
// 複数回にわたって画面遷移するうちに、後から生成された新しいgoroutineが
// 古い生き残りgoroutineに入力を横取りされ、キー入力を待ち続けたまま
// フリーズする不具合につながっていた(複数回の画面遷移で実際に再現確認済み)。
//
// 対処: 読み取り用goroutineをプログラム開始時に1つだけ生成し、
// selector.Run/pager.Start/waitForKeyなど全ての画面で使い回す。
// ブロッキングreadの途中にあるgoroutineを外部から安全に止める方法は無いため、
// 「そもそも複数生成しない」のが唯一の完全な対処になる。
package keyreader

import "bufio"

// KeyReader は標準入力などから1バイトずつ読み続ける、プロセス生存期間中ずっと
// 生きている唯一のリーダー。
type KeyReader struct {
	ch    chan byte
	errCh chan error
}

// New は指定したReaderからの読み取りgoroutineを起動する。
// プログラム全体でこの呼び出しは1回だけにすること。
func New(r interface {
	Read(p []byte) (n int, err error)
}) *KeyReader {
	kr := &KeyReader{ch: make(chan byte), errCh: make(chan error, 1)}
	br := bufio.NewReader(r)
	go func() {
		for {
			b, err := br.ReadByte()
			if err != nil {
				kr.errCh <- err
				return
			}
			kr.ch <- b
		}
	}()
	return kr
}

// Chan は読み取れた1バイトを受け取るチャネル。
func (kr *KeyReader) Chan() <-chan byte {
	return kr.ch
}

// ErrChan は読み取りエラー(EOF等)を受け取るチャネル。
func (kr *KeyReader) ErrChan() <-chan error {
	return kr.errCh
}

// ReadByte は1バイト読めるまでブロックする(タイムアウト無し)。
// selector.IOのReadKey等、単純な同期読み取りが欲しい箇所向けのヘルパー。
func (kr *KeyReader) ReadByte() (byte, error) {
	select {
	case b := <-kr.ch:
		return b, nil
	case err := <-kr.errCh:
		return 0, err
	}
}
