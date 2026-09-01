module x5ch-go

go 1.22

replace golang.org/x/text => github.com/golang/text v0.14.0

require golang.org/x/text v0.0.0-00010101000000-000000000000

require github.com/dlclark/regexp2 v1.11.0

require golang.org/x/term v0.9.0

require github.com/rivo/uniseg v0.2.0 // indirect

replace golang.org/x/term => github.com/golang/term v0.9.0

require (
	github.com/mattn/go-runewidth v0.0.15
	golang.org/x/sys v0.9.0 // indirect
)

replace golang.org/x/sys => github.com/golang/sys v0.9.0
