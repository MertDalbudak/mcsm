// Package gameplay parses Minecraft log lines into game-level events:
// player deaths (with cause + killer), chat messages (used by anti-tox
// + ban-flying detectors), and other lifecycle hints.
//
// All matchers are pure functions over a single LogEntry's message;
// callers (slot.Slot.detectGameplayEvents) wire them into the log
// broadcaster.
package gameplay

import (
	"regexp"
	"strings"
)

// DeathCause categorizes a death by who/what caused it. Used to render
// flavored messages in Discord without us re-parsing on the consumer side.
type DeathCause string

const (
	CausePlayer  DeathCause = "player"  // killed by another player
	CauseMob     DeathCause = "mob"     // killed by a mob
	CauseFall    DeathCause = "fall"
	CauseDrown   DeathCause = "drown"
	CauseFire    DeathCause = "fire"
	CauseLava    DeathCause = "lava"
	CauseExplode DeathCause = "explode"
	CauseSuffoc  DeathCause = "suffoc"
	CauseVoid    DeathCause = "void"
	CauseStarve  DeathCause = "starve"
	CauseMagic   DeathCause = "magic"
	CauseOther   DeathCause = "other"
)

// Death describes one player death.
type Death struct {
	Player  string
	Cause   DeathCause
	Killer  string // empty unless Cause is player/mob
	RawTail string // the death-message tail (everything after the player name)
}

// chatRe matches Vanilla/Paper chat: "<Player> message".
var chatRe = regexp.MustCompile(`^<([^>]+)>\s+(.*)$`)

// ChatMessage extracts a chat message from a log line. Returns ok=false
// when the line isn't chat.
func ChatMessage(message string) (player, text string, ok bool) {
	m := chatRe.FindStringSubmatch(message)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// FlyingKick matches the message vanilla emits when a player is kicked
// for flying-while-not-allowed. The format is stable across modern
// versions:
//
//	<Player> lost connection: Flying is not enabled on this server
var flyingRe = regexp.MustCompile(`^([A-Za-z0-9_]{1,16}) lost connection: Flying is not enabled on this server\b`)

func FlyingKick(message string) (player string, ok bool) {
	m := flyingRe.FindStringSubmatch(message)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// deathPatterns is an ordered list of (regex, cause, killer-extractor).
// The regex must anchor to start; the first capture group is the player.
// killerFromMatch (optional) extracts the killer name from the regex
// match groups; nil → no killer recorded.
type deathPattern struct {
	re      *regexp.Regexp
	cause   DeathCause
	killer  func([]string) string
}

func killerLast(m []string) string {
	if len(m) >= 3 {
		return m[2]
	}
	return ""
}

// playerName matches a vanilla username (3-16 chars, alphanumerics + _).
const pn = `([A-Za-z0-9_]{1,16})`

// Order matters: more-specific patterns must come before more-general ones.
// "X was killed by magic" must precede "X was killed by <mob>".
var deathPatterns = []deathPattern{
	// Specific environmental causes (must precede the generic "killed by"):
	{regexp.MustCompile(`^` + pn + ` was killed by magic`), CauseMagic, nil},

	// Killed by another entity:
	{regexp.MustCompile(`^` + pn + ` was slain by ` + pn), CauseMob, killerLast},
	{regexp.MustCompile(`^` + pn + ` was shot by ` + pn), CauseMob, killerLast},
	{regexp.MustCompile(`^` + pn + ` was killed by ` + pn), CauseMob, killerLast},
	{regexp.MustCompile(`^` + pn + ` was blown up by ` + pn), CauseExplode, killerLast},

	// Self-inflicted / environmental:
	{regexp.MustCompile(`^` + pn + ` fell from a high place`), CauseFall, nil},
	{regexp.MustCompile(`^` + pn + ` hit the ground too hard`), CauseFall, nil},
	{regexp.MustCompile(`^` + pn + ` drowned`), CauseDrown, nil},
	{regexp.MustCompile(`^` + pn + ` went up in flames`), CauseFire, nil},
	{regexp.MustCompile(`^` + pn + ` burned to death`), CauseFire, nil},
	{regexp.MustCompile(`^` + pn + ` tried to swim in lava`), CauseLava, nil},
	{regexp.MustCompile(`^` + pn + ` blew up`), CauseExplode, nil},
	{regexp.MustCompile(`^` + pn + ` suffocated in a wall`), CauseSuffoc, nil},
	{regexp.MustCompile(`^` + pn + ` fell out of the world`), CauseVoid, nil},
	{regexp.MustCompile(`^` + pn + ` starved to death`), CauseStarve, nil},

	// Starts with "<player> was" but cause is generic:
	{regexp.MustCompile(`^` + pn + ` was`), CauseOther, nil},
}

// ParseDeath inspects message and returns a Death record if the line is
// a death notification. Only INFO-level "Server thread" lines should
// be passed; the caller filters those.
//
// Note: we anchor on `^<player> ...` patterns. Chat lines look like
// `<Player> message` (with angle brackets) and won't accidentally
// match because the < eats into the regex non-match.
func ParseDeath(message string) (Death, bool) {
	if strings.HasPrefix(message, "<") {
		return Death{}, false
	}
	for _, p := range deathPatterns {
		m := p.re.FindStringSubmatch(message)
		if m == nil {
			continue
		}
		d := Death{
			Player:  m[1],
			Cause:   p.cause,
			RawTail: strings.TrimSpace(message[len(m[1]):]),
		}
		if p.killer != nil {
			d.Killer = p.killer(m)
		}
		return d, true
	}
	return Death{}, false
}
