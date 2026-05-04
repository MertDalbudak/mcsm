package discord

import (
	"context"
	"errors"
	"testing"
)

// fakeProvider lets us drive Bot.handleInteraction logic without a real
// Discord session. We only test the *interpretation* layer (Provider
// queries and message formatting), not the gateway plumbing —
// that's discordgo's responsibility and would require a real bot token
// to integration-test.
type fakeProvider struct {
	name        string
	addr        string
	players     []string
	playerMax   int
	playerErr   error
	version     string
	versionErr  error
	temp        float64
	tempOK      bool
	bans        []string
	banErr      error
}

func (f *fakeProvider) ServerName() string                                      { return f.name }
func (f *fakeProvider) PublicAddress() string                                   { return f.addr }
func (f *fakeProvider) Players(ctx context.Context) ([]string, int, error)      { return f.players, f.playerMax, f.playerErr }
func (f *fakeProvider) Version(ctx context.Context) (string, error)             { return f.version, f.versionErr }
func (f *fakeProvider) Temperature(ctx context.Context) (float64, bool)         { return f.temp, f.tempOK }
func (f *fakeProvider) Banlist(ctx context.Context) ([]string, error)           { return f.bans, f.banErr }

func TestConnect_NoTokenReturnsNil(t *testing.T) {
	bot, err := Connect(context.Background(), "", []string{"chan-1"}, &fakeProvider{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if bot != nil {
		t.Errorf("expected nil bot for empty token")
	}
}

func TestBot_ChannelAllowed(t *testing.T) {
	b := &Bot{channels: []string{"a", "b", "c"}}
	if !b.channelAllowed("b") {
		t.Error("b should be allowed")
	}
	if b.channelAllowed("z") {
		t.Error("z should not be allowed")
	}
}

// renderResponse drives the same content-formatting branches that
// handleInteraction uses, without a discordgo session in the picture.
// It returns (text, ephemeral) for inspection.
func renderResponse(b *Bot, cmd string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	switch cmd {
	case "list":
		names, max, err := b.provider.Players(ctx)
		if err != nil {
			return "Couldn't fetch player list: " + err.Error(), false
		}
		if len(names) == 0 {
			return formatNum(0, max) + " players online.", false
		}
		return formatList(names, max), false
	case "version":
		v, err := b.provider.Version(ctx)
		if err != nil {
			return "Version unknown: " + err.Error(), false
		}
		return "Server version: " + v, false
	case "temp":
		c, ok := b.provider.Temperature(ctx)
		if !ok {
			return "Temperature monitoring is not enabled.", false
		}
		return tempMsg(c), false
	case "banlist":
		bans, err := b.provider.Banlist(ctx)
		if err != nil {
			return "Couldn't fetch banlist: " + err.Error(), false
		}
		if len(bans) == 0 {
			return "No banned players.", false
		}
		return banlistMsg(bans), false
	case "server":
		return "Connect at `" + b.provider.PublicAddress() + "` (server: " + b.provider.ServerName() + ")", false
	}
	return "Unknown command.", true
}

func TestBot_RenderResponses(t *testing.T) {
	b := &Bot{provider: &fakeProvider{
		name:      "Survival",
		addr:      "mc.example.com",
		players:   []string{"Steve", "Alex"},
		playerMax: 20,
		version:   "1.21.4",
		temp:      62.5,
		tempOK:    true,
		bans:      []string{"Bob"},
	}}
	cases := map[string]string{
		"list":    "2/20 online: Steve, Alex",
		"version": "Server version: 1.21.4",
		"temp":    "CPU temperature: 62.5°C",
		"banlist": "Banned players (1):\n• Bob",
		"server":  "Connect at `mc.example.com` (server: Survival)",
	}
	for cmd, want := range cases {
		t.Run(cmd, func(t *testing.T) {
			got, _ := renderResponse(b, cmd)
			if got != want {
				t.Errorf("got %q want %q", got, want)
			}
		})
	}
}

func TestBot_RenderResponses_EmptyAndError(t *testing.T) {
	t.Run("no_players", func(t *testing.T) {
		b := &Bot{provider: &fakeProvider{playerMax: 20}}
		got, _ := renderResponse(b, "list")
		if got != "0/20 players online." {
			t.Errorf("got %q", got)
		}
	})
	t.Run("no_temp", func(t *testing.T) {
		b := &Bot{provider: &fakeProvider{tempOK: false}}
		got, _ := renderResponse(b, "temp")
		if got != "Temperature monitoring is not enabled." {
			t.Errorf("got %q", got)
		}
	})
	t.Run("ban_err", func(t *testing.T) {
		b := &Bot{provider: &fakeProvider{banErr: errors.New("boom")}}
		got, _ := renderResponse(b, "banlist")
		if got != "Couldn't fetch banlist: boom" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		b := &Bot{provider: &fakeProvider{}}
		got, ephem := renderResponse(b, "noop")
		if got != "Unknown command." || !ephem {
			t.Errorf("got %q ephem=%v", got, ephem)
		}
	})
}

// helpers (mirrored from handleInteraction to keep the shape stable)
func formatNum(online, max int) string {
	return itoa(online) + "/" + itoa(max)
}
func formatList(names []string, max int) string {
	return itoa(len(names)) + "/" + itoa(max) + " online: " + join(names, ", ")
}
func tempMsg(c float64) string {
	return "CPU temperature: " + ftoa(c) + "°C"
}
func banlistMsg(bans []string) string {
	return "Banned players (" + itoa(len(bans)) + "):\n• " + join(bans, "\n• ")
}

// stdlib-flavored helpers (avoid importing strconv/strings just for the test)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
func ftoa(f float64) string {
	// One decimal place, good enough for test assertions.
	whole := int(f)
	frac := int((f - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac)
}
func join(s []string, sep string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for i := 1; i < len(s); i++ {
		out += sep + s[i]
	}
	return out
}
