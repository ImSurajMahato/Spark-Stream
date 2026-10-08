package commands

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/catalog"
	"EverythingSuckz/fsb/internal/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/dispatcher/handlers"
	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Only configured owners can create these locks. One bot process per deployment.
var ownerLocks sync.Map

func (m *command) LoadStream(d dispatcher.Dispatcher) {
	d.AddHandler(handlers.NewMessage(nil, uploadMessage))
}
func owner(ctx *ext.Context, u *ext.Update) bool {
	return privateChat(ctx, u) && utils.Contains(config.ValueOf.AllowedUsers, u.EffectiveChat().GetID())
}
func permanent(ctx *ext.Context, slug string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s", ctx.Self.Username, slug)
}
func pendingPrompt(ctx *ext.Context, u *ext.Update, p catalog.Pending) error {
	_, err := ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("File saved (log message %d). Enter its custom slug now, or /skip for random ID. Finish this file before sending the next one.\nRandom fallback/recovery link:\n%s\n/cancel keeps the random link and closes this prompt.", p.Entry.MessageID, permanent(ctx, p.Entry.Slug))), &ext.ReplyOpts{NoWebpage: true})
	return err
}
func uploadMessage(ctx *ext.Context, u *ext.Update) error {
	if u.EffectiveMessage == nil || u.EffectiveMessage.Out || !owner(ctx, u) {
		return dispatcher.EndGroups
	}
	id := u.EffectiveChat().GetID()
	v, _ := ownerLocks.LoadOrStore(id, make(chan struct{}, 1))
	lock := v.(chan struct{})
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	default:
		ctx.Reply(u, ext.ReplyTextString("Still processing your previous message. Wait for the reply, then try again."), nil)
		return dispatcher.EndGroups
	}
	req, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if catalog.Default == nil {
		return dispatcher.EndGroups
	}
	p, err := catalog.Default.Pending(req, id)
	if err == nil {
		if u.EffectiveMessage.Media != nil {
			pendingPrompt(ctx, u, p)
			return dispatcher.EndGroups
		}
		text := strings.TrimSpace(u.EffectiveMessage.Text)
		if strings.HasPrefix(text, "/") && text != "/skip" && text != "/cancel" {
			return dispatcher.EndGroups
		}
		if text != "/skip" && text != "/cancel" {
			if !catalog.ValidSlug(text) {
				ctx.Reply(u, ext.ReplyTextString("Use 1-64 letters, numbers, _ or -. Or /skip for random ID."), nil)
				return dispatcher.EndGroups
			}
			chosen := p.Entry
			chosen.Slug = text
			if err = catalog.Default.Add(req, chosen); err != nil {
				ctx.Reply(u, ext.ReplyTextString("Slug not saved. It may belong to another file, or the database is unavailable. Try another slug or /skip. This file is still pending."), nil)
				return dispatcher.EndGroups
			}
			p.Entry.Slug = text
		}
		if err = catalog.Default.ClearPending(req, p); err != nil {
			ctx.Reply(u, ext.ReplyTextString("Link saved, but pending cleanup failed. Retry the same slug or /skip before another upload.\n"+permanent(ctx, p.Entry.Slug)), &ext.ReplyOpts{NoWebpage: true})
			return dispatcher.EndGroups
		}
		ctx.Reply(u, ext.ReplyTextString("Saved. Send the next file when ready.\n"+permanent(ctx, p.Entry.Slug)), &ext.ReplyOpts{NoWebpage: true})
		return dispatcher.EndGroups
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		ctx.Reply(u, ext.ReplyTextString("Database unavailable. Nothing uploaded; try again later."), nil)
		return dispatcher.EndGroups
	}
	if strings.HasPrefix(strings.TrimSpace(u.EffectiveMessage.Text), "/") {
		return dispatcher.EndGroups
	}
	switch u.EffectiveMessage.Media.(type) {
	case *tg.MessageMediaDocument, *tg.MessageMediaPhoto:
	default:
		ctx.Reply(u, ext.ReplyTextString("Send one file or photo. No slug is currently pending."), nil)
		return dispatcher.EndGroups
	}
	update, err := utils.ForwardMessages(ctx, id, config.ValueOf.LogChannelID, u.EffectiveMessage.ID)
	if err != nil {
		ctx.Reply(u, ext.ReplyTextString("Telegram could not save the file. Try later; check the log channel before retrying an uncertain upload."), nil)
		return dispatcher.EndGroups
	}
	// Telegram update order is not guaranteed. Search the returned channel message.
	var msg *tg.Message
	for _, upd := range update.Updates {
		if n, ok := upd.(*tg.UpdateNewChannelMessage); ok {
			if candidate, ok := n.Message.(*tg.Message); ok {
				if peer, ok := candidate.PeerID.(*tg.PeerChannel); ok && peer.ChannelID == config.ValueOf.LogChannelID {
					msg = candidate
					break
				}
			}
		}
	}
	if msg == nil {
		ctx.Reply(u, ext.ReplyTextString("Telegram returned no usable channel message. Check the log channel; recover with /setslug custom_ID log_message_ID."), nil)
		return dispatcher.EndGroups
	}
	file, err := utils.FileFromMedia(msg.Media)
	if err != nil {
		return err
	}
	slug, err := catalog.RandomSlug()
	if err != nil {
		return err
	}
	entry := catalog.Entry{Slug: slug, ChannelID: config.ValueOf.LogChannelID, MessageID: msg.ID, Hash: utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID)}
	if err = catalog.Default.Add(req, entry); err != nil {
		ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("File is in log channel as %d, but catalog save failed. Recover with /setslug custom_ID %d.", msg.ID, msg.ID)), nil)
		return dispatcher.EndGroups
	}
	p = catalog.Pending{Owner: id, Entry: entry, SourceID: u.EffectiveMessage.ID}
	if err = catalog.Default.SavePending(req, p); err != nil {
		ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("Random link saved, but slug prompt could not be saved. Use /setslug custom_ID %d if needed.\n%s", msg.ID, permanent(ctx, slug))), &ext.ReplyOpts{NoWebpage: true})
		return dispatcher.EndGroups
	}
	if err = pendingPrompt(ctx, u, p); err != nil {
		return err
	}
	return dispatcher.EndGroups
}
