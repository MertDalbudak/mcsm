package gameplay

import (
	"regexp"
	"strings"
)

// ToxicityChecker matches chat messages against a wordlist. The matcher
// is whole-word, case-insensitive, with simple obfuscation tolerance
// (collapses repeated letters: "fuuuck" → "fuck"; ignores punctuation
// between letters: "f.u.c.k" → "fuck").
//
// Wordlist content is operator-defined and lives in the per-server
// .mcsm/config.yaml under features.anti_toxicity_words.
type ToxicityChecker struct {
	matchers []*regexp.Regexp
}

func NewToxicityChecker(words []string) *ToxicityChecker {
	c := &ToxicityChecker{}
	for _, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		// Build an obfuscation-tolerant pattern: between each letter,
		// allow zero or more non-word characters; allow each letter to
		// repeat. Whole-word boundaries.
		var b strings.Builder
		b.WriteString(`(?i)\b`)
		for i, r := range w {
			if i > 0 {
				b.WriteString(`[\W_]*`)
			}
			b.WriteRune(r)
			b.WriteString(`+`)
		}
		b.WriteString(`\b`)
		re, err := regexp.Compile(b.String())
		if err == nil {
			c.matchers = append(c.matchers, re)
		}
	}
	return c
}

// Match returns true if any blacklisted word appears in text.
func (c *ToxicityChecker) Match(text string) bool {
	for _, re := range c.matchers {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// Empty reports whether the checker has any matchers; useful so the
// caller can skip subscribing to log events when nothing is configured.
func (c *ToxicityChecker) Empty() bool { return len(c.matchers) == 0 }
