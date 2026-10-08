package commands

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/catalog"
	"EverythingSuckz/fsb/internal/security"
	"EverythingSuckz/fsb/internal/utils"
	"context"
	"math/rand"
	"strconv"
	"time"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"
)

func lectureButton(ctx *ext.Context, u *ext.Update) error {
	cb := u.CallbackQuery
	if cb == nil {
		return nil
	}
	// Never allow a button copied into a group or a different user's peer.
	peer, ok := cb.Peer.(*tg.PeerUser)
	if !ok || peer.UserID != cb.UserID {
		return dispatcher.EndGroups
	}
	action, id, ok := catalog.ParseButton(string(cb.Data))
	if !ok {
		return dispatcher.EndGroups
	}
	// Acknowledge first so Telegram's button spinner stops.
	ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{QueryID: cb.QueryID, Message: "Checking lecture..."})
	fail := func(text string) error {
		_, err := ctx.SendMessage(cb.UserID, &tg.MessagesSendMessageRequest{Message: text, NoWebpage: true})
		if err != nil {
			return err
		}
		return dispatcher.EndGroups
	}
	if !botLimiter.Allow(strconv.FormatInt(cb.UserID, 10), time.Now()) {
		return fail("Too many requests. Try again shortly.")
	}
	select {
	case lookupSlots <- struct{}{}:
		defer func() { <-lookupSlots }()
	default:
		return fail("Bot busy. Try again shortly.")
	}
	req, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if catalog.Default == nil {
		return fail("Catalog unavailable.")
	}
	e, err := catalog.Default.ByMessage(req, config.ValueOf.LogChannelID, id)
	if err != nil || e.ChannelID != config.ValueOf.LogChannelID {
		return fail("Lecture unavailable. Open its permanent link again or try later.")
	}
	if action == "s" {
		link := security.Link(config.ValueOf.Host, config.ValueOf.SigningSecret, e.MessageID, e.Hash, time.Now().Add(4*time.Hour).Unix())
		return fail("Fresh stream URL (4 hours from now):\n" + link + "\n\nCopy the complete URL into MX Player Network stream or VLC Open network stream. Click Get Stream Link again after expiry.")
	}
	channel, err := utils.GetLogChannelPeer(req, ctx.Raw, ctx.PeerStorage)
	if err != nil {
		return fail("Log channel unavailable.")
	}
	result, err := ctx.Raw.ChannelsGetMessages(req, &tg.ChannelsGetMessagesRequest{Channel: channel, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: e.MessageID}}})
	if err != nil {
		return fail("File unavailable. Try later.")
	}
	// Check that catalogued message still holds the same file before delivering it.
	messages, ok := result.(*tg.MessagesChannelMessages)
	if !ok || len(messages.Messages) != 1 {
		return fail("File unavailable.")
	}
	msg, ok := messages.Messages[0].(*tg.Message)
	if !ok {
		return fail("File unavailable.")
	}
	file, err := utils.FileFromMedia(msg.Media)
	if err != nil || utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID) != e.Hash {
		return fail("File changed or is unavailable.")
	}
	to := ctx.PeerStorage.GetInputPeerById(cb.UserID)
	if to.Zero() {
		return fail("Open the bot's private chat again.")
	}
	// Server-side Telegram copy, no downloading bytes to this 512MB host.
	// Respect protected-content and flood errors: no bypass or tight retry.
	_, err = ctx.Raw.MessagesForwardMessages(req, &tg.MessagesForwardMessagesRequest{
		FromPeer: &tg.InputPeerChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash},
		ToPeer:   to, ID: []int{e.MessageID}, RandomID: []int64{rand.Int63()}, DropAuthor: true, DropMediaCaptions: true,
	})
	if err != nil {
		return fail("Telegram could not send this file (it may be protected or rate-limited). Try later or ask the owner to check channel settings.")
	}
	return dispatcher.EndGroups
}
