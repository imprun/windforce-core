package state

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLogWindowUsesByteOffsetsWithoutSplittingUTF8(t *testing.T) {
	logs := "A한글B"

	first := logWindow(logs, 0, 4)
	if first.NewLogs != "A한" || first.Offset != 4 {
		t.Fatalf("first window = %#v", first)
	}
	second := logWindow(logs, first.Offset, 4)
	if second.NewLogs != "글B" || second.Offset != int64(len([]byte(logs))) {
		t.Fatalf("second window = %#v", second)
	}
	insideRune := logWindow(logs, 2, 4)
	if !utf8.ValidString(insideRune.NewLogs) || insideRune.NewLogs != "글B" {
		t.Fatalf("inside-rune window = %#v", insideRune)
	}
	smallLimit := logWindow("한", 0, 1)
	if smallLimit.NewLogs != "한" || smallLimit.Offset != 3 {
		t.Fatalf("small-limit window did not make progress = %#v", smallLimit)
	}
}

func TestLogWindowClampsOffsetsAndUnlimitedReads(t *testing.T) {
	if got := logWindow("hello", -1, 0); got.NewLogs != "hello" || got.Offset != 5 {
		t.Fatalf("negative offset window = %#v", got)
	}
	if got := logWindow("hello", 99, 2); got.NewLogs != "" || got.Offset != 5 {
		t.Fatalf("past-end window = %#v", got)
	}
	invalid := "ok" + string([]byte{0xff})
	if got := logWindow(strings.ToValidUTF8(invalid, "\uFFFD"), 0, 0); !utf8.ValidString(got.NewLogs) {
		t.Fatalf("normalized window is invalid UTF-8: %#v", got)
	}
}
