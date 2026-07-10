package jobs

import (
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"short", "hello", "hello"},
		{"at_limit", strings.Repeat("a", MaxOutputBytes), strings.Repeat("a", MaxOutputBytes)},
		{"over_limit", strings.Repeat("a", MaxOutputBytes+100), strings.Repeat("a", MaxOutputBytes) + TruncateSuffix},
		{"newline_at_end_preserved", "line1\nline2\n", "line1\nline2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncate(c.in)
			if got != c.want {
				t.Fatalf("truncate mismatch:\n got len=%d\nwant len=%d", len(got), len(c.want))
			}
		})
	}
}

func TestClassifyResult(t *testing.T) {
	cases := []struct {
		name     string
		exit     int
		err      string
		wantItem string
	}{
		{"zero_exit", 0, "", ItemCompleted},
		{"zero_exit_with_unrelated_err", 0, "anything", ItemFailed},
		{"nonzero_exit", 1, "", ItemFailed},
		{"nonzero_exit_with_msg", 127, "command not found", ItemFailed},
		{"timeout", 124, "timeout", ItemTimeout}, // 124 is bash convention for timeout
		{"timeout_any_exit", -1, "timeout", ItemTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyResult(c.exit, c.err)
			if got != c.wantItem {
				t.Fatalf("classifyResult(exit=%d, err=%q) = %q, want %q",
					c.exit, c.err, got, c.wantItem)
			}
		})
	}
}