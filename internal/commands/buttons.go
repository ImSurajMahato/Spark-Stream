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
	"github.com/gotd/td/tgerr"
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
	// Persist deletion intent BEFORE delivery. Refuse to send if MongoDB fails.
	recipient, ok := to.(*tg.InputPeerUser)
	if !ok {
		return fail("Open the bot's private chat again.")
	}
	jobID, err := catalog.RandomSlug()
	if err != nil {
		return fail("Could not prepare file delivery.")
	}
	job := catalog.NewDelivery(jobID, cb.UserID, recipient.AccessHash, e.ChannelID, rand.Int63(), e.MessageID, e.Hash, time.Now())
	if err = catalog.Default.QueueDelivery(req, job); err != nil {
		return fail("Could not save auto-delete job. No file sent; try later.")
	}
	copiedID, err := sendProtected(req, ctx, job)
	if err != nil {
		if wait, ok := tgerr.AsFloodWait(err); ok {
			retryCtx, retryCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = catalog.Default.RetryDelivery(retryCtx, job, wait, false, "flood_wait")
			retryCancel()
		}
		return fail("Telegram did not confirm delivery. The bot will retry with the same delivery ID; do not repeatedly click. Ask the owner if no file arrives.")
	}
	if err = catalog.Default.DeliverySent(req, job, copiedID); err != nil {
		return fail("File sent with content protection, but auto-delete confirmation is pending. Ask the owner to check the deletion queue.")
	}
	return fail("Protected file sent. Auto-delete is scheduled for 4 hours from this delivery request. Normal Telegram forwarding/saving is disabled. Deletion can be late if the bot is offline. This is not DRM.")
}
