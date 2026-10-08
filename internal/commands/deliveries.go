package commands

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/catalog"
	"EverythingSuckz/fsb/internal/utils"
	"context"
	"errors"
	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
	"time"
)

func deliveredID(result tg.UpdatesClass, random int64) int {
	var updates []tg.UpdateClass
	switch r := result.(type) {
	case *tg.Updates:
		updates = r.Updates
	case *tg.UpdatesCombined:
		updates = r.Updates
	default:
		return 0
	}
	for _, u := range updates {
		if m, ok := u.(*tg.UpdateMessageID); ok && m.RandomID == random {
			return m.ID
		}
	}
	// Fail closed without exact random_id mapping; never guess another message ID.
	return 0
}
func sendProtected(ctx context.Context, bot *ext.Context, d catalog.Delivery) (int, error) {
	if d.ChannelID != config.ValueOf.LogChannelID {
		return 0, errors.New("log channel changed")
	}
	channel, err := utils.GetLogChannelPeer(ctx, bot.Raw, bot.PeerStorage)
	if err != nil {
		return 0, err
	}
	source, err := bot.Raw.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: channel, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: d.SourceID}}})
	if err != nil {
		return 0, err
	}
	messages, ok := source.(*tg.MessagesChannelMessages)
	if !ok || len(messages.Messages) != 1 {
		return 0, errors.New("source unavailable")
	}
	msg, ok := messages.Messages[0].(*tg.Message)
	if !ok {
		return 0, errors.New("source unavailable")
	}
	file, err := utils.FileFromMedia(msg.Media)
	if err != nil || utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID) != d.Hash {
		return 0, errors.New("source changed")
	}
	updates, err := bot.Raw.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: &tg.InputPeerChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash},
		ToPeer:   &tg.InputPeerUser{UserID: d.UserID, AccessHash: d.AccessHash}, ID: []int{d.SourceID}, RandomID: []int64{d.RandomID}, DropAuthor: true, DropMediaCaptions: true, Noforwards: true,
	})
	if err != nil {
		return 0, err
	}
	id := deliveredID(updates, d.RandomID)
	if id <= 0 {
		return 0, errors.New("Telegram did not return the copied message ID")
	}
	return id, nil
}

// One bot replica only. Poll immediately after startup, then every minute.
// Sleeping hosts cannot delete on time. Persistent intents/jobs recover after restart.
func RunFileDeletion(ctx context.Context, bot *ext.Context, log *zap.Logger) {
	var floodUntil time.Time
	sweep := func() {
		if time.Now().Before(floodUntil) {
			return
		}
		req, cancel := context.WithTimeout(ctx, 10*time.Second)
		jobs, err := catalog.Default.DueDeliveries(req, time.Now().UTC())
		cancel()
		if err != nil {
			log.Warn("File deletion queue unavailable")
			return
		}
		for _, d := range jobs {
			if ctx.Err() != nil {
				return
			}
			now := time.Now().UTC()
			// Do not redeliver a crash-recovery intent after its four-hour deadline.
			if d.State == "sending" && !now.Before(d.DeleteAt) {
				req, cancel := context.WithTimeout(ctx, 10*time.Second)
				err = catalog.Default.RetryDelivery(req, d, 0, true, "unresolved_send_expired")
				cancel()
				log.Warn("Unresolved file delivery needs manual review", zap.String("job", d.ID), zap.Bool("recorded", err == nil))
				continue
			}
			req, cancel := context.WithTimeout(ctx, 20*time.Second)
			if d.State == "sending" {
				var id int
				id, err = sendProtected(req, bot, d)
				if err == nil {
					err = catalog.Default.DeliverySent(req, d, id)
				}
			} else {
				_, err = bot.Raw.MessagesDeleteMessages(req, &tg.MessagesDeleteMessagesRequest{ID: []int{d.MessageID}, Revoke: true})
				if err == nil {
					err = catalog.Default.FinishDelivery(req, d.ID)
				}
			}
			cancel()
			if err == nil {
				continue
			}
			delay := catalog.RetryDelay(d.Attempts)
			if wait, ok := tgerr.AsFloodWait(err); ok {
				if wait > delay {
					delay = wait
				}
			}
			terminal := tgerr.Is(err, "MESSAGE_DELETE_FORBIDDEN", "CHAT_WRITE_FORBIDDEN", "USER_IS_BLOCKED", "INPUT_USER_DEACTIVATED", "CHAT_FORWARDS_RESTRICTED", "PEER_ID_INVALID") || now.Sub(d.CreatedAt) >= 48*time.Hour
			reason := "retry"
			if terminal {
				reason = "telegram_refused_or_48h_elapsed"
			}
			// Do not log errors containing connection strings, tokens or access hashes.
			req, cancel = context.WithTimeout(ctx, 10*time.Second)
			saved := catalog.Default.RetryDelivery(req, d, delay, terminal, reason)
			cancel()
			log.Warn("File delivery/deletion retry", zap.String("job", d.ID), zap.Bool("terminal", terminal), zap.Bool("recorded", saved == nil))
			// Respect global Telegram flood limits instead of processing the rest of the batch.
			if wait, ok := tgerr.AsFloodWait(err); ok {
				floodUntil = time.Now().Add(wait)
				return
			}
		}
	}
	sweep()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
