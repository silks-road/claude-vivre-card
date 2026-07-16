package notifier

import (
	"regexp"
	"strings"

	"github.com/777genius/claude-notifications/internal/analyzer"
)

var (
	codeFenceRe    = regexp.MustCompile("(?s)```.*?```")
	inlineCodeRe   = regexp.MustCompile("`([^`]*)`")
	mdLinkRe       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdEmphasisRe   = regexp.MustCompile(`(\*\*|__|\*|_|~~)`)
	mdHeadingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+(.*)$`)
	mdListMarkerRe = regexp.MustCompile(`(?m)^\s*([-*+]|\d+\.)\s+`)
	mdQuoteRe      = regexp.MustCompile(`(?m)^\s*>\s?`)
	whitespaceRe   = regexp.MustCompile(`\s+`)
	sentenceRe     = regexp.MustCompile(`[^.!?]*[.!?]`)
	emojiRe        = regexp.MustCompile(`[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2B00}-\x{2BFF}\x{FE0F}\x{200D}]`)
)

const summaryMaxLen = 110

// isAskingStatus reports whether the notification is about the user being
// needed — then the body should show what is being ASKED, not the opening line.
func isAskingStatus(status analyzer.Status) bool {
	switch status {
	case analyzer.StatusQuestion, analyzer.StatusApprovalNeeded, analyzer.StatusPlanReady:
		return true
	}
	return false
}

// summarizeMessage reduces an assistant message to a single clean sentence for
// the small notification body. Markdown is stripped (headings become their own
// sentences, emoji removed — the title already carries the status emoji).
// For asking statuses the LAST question sentence is picked, so a message that
// reports work and then asks something shows the question, matching the
// "Needs you" title. Otherwise the first sentence is used.
func summarizeMessage(message string, status analyzer.Status) string {
	s := message
	s = codeFenceRe.ReplaceAllString(s, " ")
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = inlineCodeRe.ReplaceAllString(s, "$1")
	s = mdHeadingRe.ReplaceAllString(s, "$1. ")
	s = mdListMarkerRe.ReplaceAllString(s, "")
	s = mdQuoteRe.ReplaceAllString(s, "")
	s = mdEmphasisRe.ReplaceAllString(s, "")
	s = emojiRe.ReplaceAllString(s, "")
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return message
	}

	sentences := sentenceRe.FindAllString(s, -1)
	picked := ""
	if isAskingStatus(status) {
		// Last question wins; questions usually close the message.
		for i := len(sentences) - 1; i >= 0; i-- {
			if strings.HasSuffix(strings.TrimSpace(sentences[i]), "?") {
				picked = strings.TrimSpace(sentences[i])
				break
			}
		}
	}
	if picked == "" {
		if len(sentences) > 0 {
			picked = strings.TrimSpace(sentences[0])
		} else {
			picked = s
		}
	}

	if len(picked) > summaryMaxLen {
		cut := picked[:summaryMaxLen]
		if i := strings.LastIndex(cut, " "); i > summaryMaxLen/2 {
			cut = cut[:i]
		}
		picked = strings.TrimRight(cut, " ,;:") + "…"
	}
	return picked
}
