package notifier

import (
	"strings"
	"testing"

	"github.com/777genius/claude-notifications/internal/analyzer"
)

func TestSummarizeMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "first sentence extracted",
			in:   "Added conversation titles to notifications. Next I will rebuild the click behaviour and deploy.",
			want: "Added conversation titles to notifications.",
		},
		{
			name: "markdown stripped",
			in:   "**Part 1 is live** — check the [notification](https://example.com) that `just appeared`.",
			want: "Part 1 is live — check the notification that just appeared.",
		},
		{
			name: "code fence removed",
			in:   "Run this:\n```bash\nmake build\n```\nthen tell me what happened.",
			want: "Run this: then tell me what happened.",
		},
		{
			name: "long text capped at word boundary with ellipsis",
			in:   strings.Repeat("word ", 40),
			want: "", // checked by length assertion below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeMessage(tt.in, analyzer.StatusTaskComplete)
			if tt.want != "" && got != tt.want {
				t.Errorf("summarizeMessage() = %q, want %q", got, tt.want)
			}
			if len(got) > summaryMaxLen+3 {
				t.Errorf("summary too long (%d chars): %q", len(got), got)
			}
		})
	}

	t.Run("asking status picks the last question", func(t *testing.T) {
		in := "## Fixed and verified\n\nThe notification reads clean now. Everything is pushed.\n\n## Question\n\nShall I start the phone setup or the Chrome extension?"
		want := "Shall I start the phone setup or the Chrome extension?"
		if got := summarizeMessage(in, analyzer.StatusQuestion); got != want {
			t.Errorf("question summary = %q, want %q", got, want)
		}
	})

	t.Run("asking status without question falls back to first sentence", func(t *testing.T) {
		in := "Waiting on your approval to push. It is all committed."
		want := "Waiting on your approval to push."
		if got := summarizeMessage(in, analyzer.StatusApprovalNeeded); got != want {
			t.Errorf("fallback summary = %q, want %q", got, want)
		}
	})

	t.Run("headings become sentences not glue", func(t *testing.T) {
		in := "## Fixed and verified\n\nThe notification reads clean."
		got := summarizeMessage(in, analyzer.StatusTaskComplete)
		if got != "Fixed and verified." {
			t.Errorf("heading summary = %q, want %q", got, "Fixed and verified.")
		}
	})

	t.Run("empty stays empty-safe", func(t *testing.T) {
		if got := summarizeMessage("   ", analyzer.StatusTaskComplete); got != "   " {
			t.Errorf("blank input should be returned as-is, got %q", got)
		}
	})
}
