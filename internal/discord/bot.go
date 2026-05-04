// Package discord runs one Discord bot session per mounted Minecraft
// server. Each bot:
//
//   - Posts notifications to its configured channels (server start/stop,
//     deaths if enabled, anti-toxicity warnings, etc.).
//   - Registers slash commands (/list, /version, /temp, /banlist, /server)
//     and answers them by querying the slot or the system.
//
// The session is owned by the slot — the slot's Start path constructs
// a Bot via Connect(), and Stop calls Bot.Close(). Operating without a
// configured token is a no-op (Connect returns nil, nil).
//
// We deliberately keep this package thin: it knows nothing about how
// to fetch player lists or temperatures — those come in via the
// Provider interface so tests can stub them and we don't pull in slot
// internals here.
package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Provider is everything the bot needs to read from the live server +
// host. The slot package's adapter implements this with RCON + SLP +
// the system temperature sampler.
type Provider interface {
	ServerName() string
	PublicAddress() string
	Players(ctx context.Context) ([]string, int, error) // names, max
	Version(ctx context.Context) (string, error)
	Temperature(ctx context.Context) (float64, bool)    // (celsius, available)
	Banlist(ctx context.Context) ([]string, error)
}

// Bot is a connected session. Methods are goroutine-safe.
type Bot struct {
	sess     *discordgo.Session
	channels []string
	provider Provider
	cleanup  func() // unregisters interaction handler
	mu       sync.Mutex
	closed   bool
}

// ErrNoToken signals that no token is configured — Connect returns
// (nil, nil) for empty-token cases so the slot can treat Discord as
// optional without conditionals at every callsite.
var ErrNoToken = errors.New("no discord token configured")

// Connect opens the gateway and registers the configured slash commands
// once the session is ready. Returns (nil, nil) when token is empty.
func Connect(ctx context.Context, token string, channels []string, p Provider) (*Bot, error) {
	if token == "" {
		return nil, nil
	}
	sess, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord: new session: %w", err)
	}
	sess.Identify.Intents = discordgo.IntentsGuildMessages
	if err := sess.Open(); err != nil {
		return nil, fmt.Errorf("discord: open: %w", err)
	}

	bot := &Bot{
		sess:     sess,
		channels: append([]string(nil), channels...),
		provider: p,
	}
	bot.cleanup = sess.AddHandler(bot.handleInteraction)

	// Register slash commands per guild. We register them on every guild
	// any of our channels live in. Best-effort: failures are logged.
	if err := bot.registerCommands(); err != nil {
		slog.Warn("discord: register commands", "err", err)
	}

	slog.Info("discord: connected", "channels", len(channels))
	return bot, nil
}

// Close shuts down the session. Safe to call multiple times.
func (b *Bot) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.cleanup != nil {
		b.cleanup()
	}
	if b.sess != nil {
		return b.sess.Close()
	}
	return nil
}

// Notify sends msg to every configured channel. Errors per channel are
// logged but never propagated — notifications are best-effort.
func (b *Bot) Notify(ctx context.Context, msg string) {
	b.mu.Lock()
	if b.closed || b.sess == nil {
		b.mu.Unlock()
		return
	}
	chans := append([]string(nil), b.channels...)
	sess := b.sess
	b.mu.Unlock()

	for _, ch := range chans {
		if _, err := sess.ChannelMessageSend(ch, msg); err != nil {
			slog.Warn("discord: notify", "channel", ch, "err", err)
		}
	}
}

// handleInteraction routes Discord slash-command interactions. Anything
// we don't recognize is acknowledged with a short error.
func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	if !b.channelAllowed(i.ChannelID) {
		respond(s, i, "This bot isn't linked to this channel.", true)
		return
	}
	cmd := i.ApplicationCommandData().Name
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch cmd {
	case "list":
		names, max, err := b.provider.Players(ctx)
		if err != nil {
			respond(s, i, "Couldn't fetch player list: "+err.Error(), false)
			return
		}
		if len(names) == 0 {
			respond(s, i, fmt.Sprintf("0/%d players online.", max), false)
			return
		}
		respond(s, i, fmt.Sprintf("%d/%d online: %s", len(names), max, strings.Join(names, ", ")), false)
	case "version":
		v, err := b.provider.Version(ctx)
		if err != nil {
			respond(s, i, "Version unknown: "+err.Error(), false)
			return
		}
		respond(s, i, "Server version: "+v, false)
	case "temp":
		c, ok := b.provider.Temperature(ctx)
		if !ok {
			respond(s, i, "Temperature monitoring is not enabled.", false)
			return
		}
		respond(s, i, fmt.Sprintf("CPU temperature: %.1f°C", c), false)
	case "banlist":
		bans, err := b.provider.Banlist(ctx)
		if err != nil {
			respond(s, i, "Couldn't fetch banlist: "+err.Error(), false)
			return
		}
		if len(bans) == 0 {
			respond(s, i, "No banned players.", false)
			return
		}
		respond(s, i, fmt.Sprintf("Banned players (%d):\n• %s", len(bans), strings.Join(bans, "\n• ")), false)
	case "server":
		respond(s, i, fmt.Sprintf("Connect at `%s` (server: %s)",
			b.provider.PublicAddress(), b.provider.ServerName()), false)
	default:
		respond(s, i, "Unknown command.", true)
	}
}

func (b *Bot) channelAllowed(id string) bool {
	for _, ch := range b.channels {
		if ch == id {
			return true
		}
	}
	return false
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: flags},
	})
}

// registerCommands creates the slash commands on every guild that
// contains any of our configured channels. We deduplicate guilds.
func (b *Bot) registerCommands() error {
	defs := []*discordgo.ApplicationCommand{
		{Name: "list", Description: "List online players"},
		{Name: "version", Description: "Show server version"},
		{Name: "temp", Description: "Show host CPU temperature"},
		{Name: "banlist", Description: "List banned players"},
		{Name: "server", Description: "How to connect to the server"},
	}

	guilds := map[string]bool{}
	for _, ch := range b.channels {
		c, err := b.sess.Channel(ch)
		if err != nil {
			slog.Warn("discord: lookup channel", "channel", ch, "err", err)
			continue
		}
		guilds[c.GuildID] = true
	}
	for guildID := range guilds {
		for _, d := range defs {
			if _, err := b.sess.ApplicationCommandCreate(b.sess.State.User.ID, guildID, d); err != nil {
				slog.Warn("discord: register command",
					"guild", guildID, "command", d.Name, "err", err)
			}
		}
	}
	return nil
}
