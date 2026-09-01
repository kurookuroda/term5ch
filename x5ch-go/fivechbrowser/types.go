package fivechbrowser

// Board は5chの1つの板を表す。Ruby版の { title:, url: } に対応。
type Board struct {
	Title string
	URL   string
}

// Category はメニュー上の1カテゴリと、そこに属する板一覧。
// Ruby版の { title:, boards: [...] } に対応。
type Category struct {
	Title  string
	Boards []Board
}

// ThreadInfo は1スレッドの識別情報と、閲覧に伴って更新される状態を保持する。
type ThreadInfo struct {
	DatFile  string
	Title    string
	Count    int
	Ikioi    float64
	BoardURL string
	LastRead int

	URL string
}

// HasNew は未読レスがあるかどうかを都度計算する。
// Ruby版のようにフィールドとして保存すると、CountやLastReadの更新時に
// 同期し忘れて古い値が残るバグを避けられる。
func (t ThreadInfo) HasNew() bool {
	return t.Count > t.LastRead
}

// Post は1レスを表す。
type Post struct {
	Num     int
	Name    string
	Date    string
	Message string
}
