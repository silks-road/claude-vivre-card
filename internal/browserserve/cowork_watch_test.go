//go:build darwin

package browserserve

import "testing"

func TestSessionIsHookCovered(t *testing.T) {
	log := []byte("2026-07-21 [info] [Stop hook] Query completed for session local_LOCAL123\n" +
		"2026-07-21 [info] [Result] Turn succeeded for session local_LOCAL123\n" +
		"2026-07-21 [info] [Result] Turn succeeded for session local_HOMETASK9\n")
	if !sessionIsHookCovered(log, "local_LOCAL123") {
		t.Error("local session with Stop-hook line should be hook-covered")
	}
	if sessionIsHookCovered(log, "local_HOMETASK9") {
		t.Error("Home task without Stop-hook line must NOT be hook-covered")
	}
}

func TestTitleFromLogData(t *testing.T) {
	log := []byte("Updated session local_3047df7f-7754-4a6c: { title: 'Old Title', titleSource: 'auto' }\n" +
		"Updated session local_3047df7f-7754-4a6c: { title: 'External memory system setup', titleSource: 'auto' }\n")
	if got := titleFromLogData(log, "local_3047df7f-7754-4a6c"); got != "External memory system setup" {
		t.Errorf("title = %q, want latest 'External memory system setup'", got)
	}
	if got := titleFromLogData(log, "local_missing"); got != "" {
		t.Errorf("title for unknown = %q, want empty", got)
	}
}
