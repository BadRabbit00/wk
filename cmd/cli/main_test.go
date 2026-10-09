package main

import (
	"os"
	"testing"
)

func TestMainWiring(t *testing.T) {
	oldExit, oldArgs := exitProcess, os.Args
	t.Cleanup(func() { exitProcess = oldExit; os.Args = oldArgs })
	for _, tc := range []struct {
		arg  string
		want int
	}{{"help", 0}, {"unknown", 2}} {
		code := -1
		exitProcess = func(c int) { code = c }
		os.Args = []string{"wk", tc.arg}
		main()
		if code != tc.want {
			t.Fatalf("exit=%d want=%d", code, tc.want)
		}
	}
}
