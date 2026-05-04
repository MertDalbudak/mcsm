package gameplay

import "testing"

func TestParseDeath(t *testing.T) {
	cases := []struct {
		msg    string
		want   bool
		player string
		cause  DeathCause
		killer string
	}{
		{"Steve was slain by Zombie", true, "Steve", CauseMob, "Zombie"},
		{"Alex was shot by Skeleton", true, "Alex", CauseMob, "Skeleton"},
		{"Bob was killed by Creeper", true, "Bob", CauseMob, "Creeper"},
		{"Mert was blown up by Creeper", true, "Mert", CauseExplode, "Creeper"},
		{"Steve fell from a high place", true, "Steve", CauseFall, ""},
		{"Steve hit the ground too hard", true, "Steve", CauseFall, ""},
		{"Steve drowned", true, "Steve", CauseDrown, ""},
		{"Alex went up in flames", true, "Alex", CauseFire, ""},
		{"Alex burned to death", true, "Alex", CauseFire, ""},
		{"Steve tried to swim in lava", true, "Steve", CauseLava, ""},
		{"Steve blew up", true, "Steve", CauseExplode, ""},
		{"Steve suffocated in a wall", true, "Steve", CauseSuffoc, ""},
		{"Alex fell out of the world", true, "Alex", CauseVoid, ""},
		{"Alex starved to death", true, "Alex", CauseStarve, ""},
		{"Alex was killed by magic", true, "Alex", CauseMagic, ""},
		{"<Steve> was slain by Zombie", false, "", "", ""}, // chat → not a death
		{"Steve joined the game", false, "", "", ""},
		{"random log line", false, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			d, ok := ParseDeath(tc.msg)
			if ok != tc.want {
				t.Fatalf("ok=%v want=%v (got %+v)", ok, tc.want, d)
			}
			if !ok {
				return
			}
			if d.Player != tc.player {
				t.Errorf("player: got %q want %q", d.Player, tc.player)
			}
			if d.Cause != tc.cause {
				t.Errorf("cause: got %q want %q", d.Cause, tc.cause)
			}
			if d.Killer != tc.killer {
				t.Errorf("killer: got %q want %q", d.Killer, tc.killer)
			}
		})
	}
}

func TestChatMessage(t *testing.T) {
	cases := []struct {
		in        string
		ok        bool
		player    string
		text      string
	}{
		{"<Steve> hello world", true, "Steve", "hello world"},
		{"<Player_1> /tp ~", true, "Player_1", "/tp ~"},
		{"Steve joined the game", false, "", ""},
		{"<>", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			p, txt, ok := ChatMessage(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok=%v want=%v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if p != tc.player || txt != tc.text {
				t.Errorf("got (%q,%q) want (%q,%q)", p, txt, tc.player, tc.text)
			}
		})
	}
}

func TestFlyingKick(t *testing.T) {
	cases := []struct {
		in     string
		ok     bool
		player string
	}{
		{"Steve lost connection: Flying is not enabled on this server", true, "Steve"},
		{"Mert lost connection: Flying is not enabled on this server, kicked", true, "Mert"},
		{"Steve lost connection: Disconnected", false, ""},
		{"Steve joined the game", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			p, ok := FlyingKick(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok=%v want=%v", ok, tc.ok)
			}
			if p != tc.player {
				t.Errorf("player: %q want %q", p, tc.player)
			}
		})
	}
}

func TestToxicityChecker(t *testing.T) {
	c := NewToxicityChecker([]string{"badword", "slur"})
	cases := []struct {
		in   string
		want bool
	}{
		{"this is fine", false},
		{"that was a badword", true},
		{"BADWORD", true},                // case insensitive
		{"baaadword", true},              // letter repetition
		{"b.a.d.w.o.r.d", true},          // punctuation between letters
		{"badwordsmith", false},          // word boundary — not a substring match
		{"the slur was thrown", true},
		{"badwords are everywhere", false}, // because of the + at end requiring boundary, "badwords" matches "badword" + s? Actually \b after + means after the last letter. Let me reconsider.
	}
	// Note: "badwords" — the regex is \bb+a+d+w+o+r+d+\b. After matching "badword",
	// the \b requires a non-word boundary. Next char is 's' (word), so no boundary
	// → no match. So "badwords" should NOT match. Test expectation ok.
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := c.Match(tc.in); got != tc.want {
				t.Errorf("Match(%q): got %v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestToxicityChecker_Empty(t *testing.T) {
	c := NewToxicityChecker(nil)
	if !c.Empty() {
		t.Error("nil words should produce empty checker")
	}
	if c.Match("anything") {
		t.Error("empty checker should never match")
	}
	c2 := NewToxicityChecker([]string{"", "  "})
	if !c2.Empty() {
		t.Error("blank words should produce empty checker")
	}
}
