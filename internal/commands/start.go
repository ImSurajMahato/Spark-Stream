package commands

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/catalog"
	"EverythingSuckz/fsb/internal/utils"
	"context"
	"fmt"
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/dispatcher/handlers"
	"github.com/celestix/gotgproto/ext"
	"github.com/celestix/gotgproto/storage"
	"github.com/gotd/td/tg"
	"strconv"
	"strings"
	"time"
)

var botLimiter = security.NewLimiter(12, 4, 4096)
var lookupSlots = make(chan struct{}, 8)

func (m *command) LoadStart(d dispatcher.Dispatcher) {
	d.AddHandler(handlers.NewCommand("start", start))
	d.AddHandler(handlers.NewCommand("setslug", setSlug))
	d.AddHandler(handlers.NewCallbackQuery(nil, lectureButton))
}
func privateChat(ctx *ext.Context, u *ext.Update) bool {
	return ctx.PeerStorage.GetPeerById(u.EffectiveChat().GetID()).Type == int(storage.TypeUser)
}
func start(ctx *ext.Context, u *ext.Update) error {
	if !privateChat(ctx, u) {
		return dispatcher.EndGroups
	}
	args := strings.Fields(u.EffectiveMessage.Text)
	if len(args) == 1 {
		ctx.Reply(u, ext.ReplyTextString("Open a lecture link, then choose Get Stream Link or Get File. Owners: send one file and enter its slug when asked, or /skip for random ID."), nil)
		return dispatcher.EndGroups
	}
	if len(args) != 2 || !catalog.ValidSlug(args[1]) {
		ctx.Reply(u, ext.ReplyTextString("Invalid lecture ID."), nil)
		return dispatcher.EndGroups
	}
	if !botLimiter.Allow(strconv.FormatInt(u.EffectiveChat().GetID(), 10), time.Now()) {
		ctx.Reply(u, ext.ReplyTextString("Too many requests. Try again shortly."), nil)
		return dispatcher.EndGroups
	}
	select {
	case lookupSlots <- struct{}{}:
		defer func() { <-lookupSlots }()
	default:
		ctx.Reply(u, ext.ReplyTextString("Bot busy. Try again shortly."), nil)
		return dispatcher.EndGroups
	}
	dbctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if catalog.Default == nil {
		return dispatcher.EndGroups
	}
	entry, err := catalog.Default.Get(dbctx, args[1])
	if err != nil || entry.ChannelID != config.ValueOf.LogChannelID {
		ctx.Reply(u, ext.ReplyTextString("Lecture unavailable. Check the ID or try again later."), nil)
		return dispatcher.EndGroups
	}
	markup := &tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: []tg.KeyboardButtonClass{
		&tg.KeyboardButtonCallback{Text: "Get Stream Link", Data: []byte(fmt.Sprintf("s:%d", entry.MessageID))},
		&tg.KeyboardButtonCallback{Text: "Get File", Data: []byte(fmt.Sprintf("f:%d", entry.MessageID))},
	}}}}
	_, err = ctx.Reply(u, ext.ReplyTextString("Choose how to open this lecture. Stream links last 4 hours from button click. Get File sends a Telegram copy; that copy does not expire."), &ext.ReplyOpts{Markup: markup, NoWebpage: true})
	if err != nil {
		return err
	}
	return dispatcher.EndGroups
}

func setSlug(ctx *ext.Context, u *ext.Update) error {
	id := u.EffectiveChat().GetID()
	if !privateChat(ctx, u) || !utils.Contains(config.ValueOf.AllowedUsers, id) {
		return dispatcher.EndGroups
	}
	args := strings.Fields(u.EffectiveMessage.Text)
	if len(args) != 3 || !catalog.ValidSlug(args[1]) {
		ctx.Reply(u, ext.ReplyTextString("Usage: /setslug custom_ID log_channel_message_ID"), nil)
		return dispatcher.EndGroups
	}
	messageID, err := strconv.Atoi(args[2])
	if err != nil || messageID <= 0 {
		return dispatcher.EndGroups
	}
	req, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	peer, err := utils.GetLogChannelPeer(req, ctx.Raw, ctx.PeerStorage)
	if err != nil {
		ctx.Reply(u, ext.ReplyTextString("Log channel unavailable."), nil)
		return dispatcher.EndGroups
	}
	result, err := ctx.Raw.ChannelsGetMessages(req, &tg.ChannelsGetMessagesRequest{Channel: peer, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}}})
	if err != nil {
		ctx.Reply(u, ext.ReplyTextString("File unavailable."), nil)
		return dispatcher.EndGroups
	}
	messages, ok := result.(*tg.MessagesChannelMessages)
	if !ok || len(messages.Messages) != 1 {
		return dispatcher.EndGroups
	}
	msg, ok := messages.Messages[0].(*tg.Message)
	if !ok {
		return dispatcher.EndGroups
	}
	file, err := utils.FileFromMedia(msg.Media)
	if err != nil {
		return dispatcher.EndGroups
	}
	hash := utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID)
	err = catalog.Default.Add(req, catalog.Entry{Slug: args[1], ChannelID: config.ValueOf.LogChannelID, MessageID: messageID, Hash: hash})
	if err != nil {
		ctx.Reply(u, ext.ReplyTextString("Could not save slug. It may already belong to another file; choose another ID."), nil)
		return dispatcher.EndGroups
	}
	ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("Permanent lecture link:\nhttps://t.me/%s?start=%s", ctx.Self.Username, args[1])), &ext.ReplyOpts{NoWebpage: true})
	return dispatcher.EndGroups
}
