// Spark Stream additions. AGPL-3.0, see LICENSE and NOTICE.
package commands

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/catalog"
	"EverythingSuckz/fsb/internal/utils"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/dispatcher/handlers"
	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const (
	scanBatch        = 100
	progressEvery    = 1000 // message ids between progress updates
	maxScanFlood     = 5 * time.Minute
	scanPause        = 250 * time.Millisecond
	removeConfirmTTL = 60 * time.Second
	removeWord       = "CONFIRM"
)

// One scan at a time per process. Two services may scan the same channel; the
// catalog skips duplicates and the checkpoint only moves forward.
var scanning atomic.Bool

var (
	removeMu    sync.Mutex
	removeArmed = map[int64]time.Time{}
)

func (m *command) LoadData(d dispatcher.Dispatcher) {
	d.AddHandler(handlers.NewCommand("data", dataCommand))
	d.AddHandler(handlers.NewCommand("remove", removeCommand))
}

func notify(ctx *ext.Context, chat int64, text string) {
	peer := ctx.PeerStorage.GetInputPeerById(chat)
	if peer.Zero() {
		return
	}
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = ctx.Raw.MessagesSendMessage(c, &tg.MessagesSendMessageRequest{Peer: peer, Message: text, RandomID: rand.Int63()})
}

// withFlood runs fn and, on FLOOD_WAIT, sleeps what Telegram asks (capped) and retries.
func withFlood[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < 6; attempt++ {
		v, err := fn()
		if err == nil {
			return v, nil
		}
		wait, ok := tgerr.AsFloodWait(err)
		if !ok {
			return zero, err
		}
		if wait > maxScanFlood {
			return zero, fmt.Errorf("telegram asked to wait %s: %w", wait.Round(time.Second), err)
		}
		select {
		case <-time.After(wait + time.Second):
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}
	return zero, errors.New("too many FLOOD_WAIT retries")
}

const adminHelp = "Cannot read the log channel. Make this bot an admin of the log channel (with permission to post messages), then send /data again."

// topMessageID posts a short message to the log channel, reads its id, and
// deletes it. Bots cannot read channel history, so this is how we learn the newest id.
func topMessageID(ctx context.Context, ctx2 *ext.Context) (int, error) {
	ch, err := utils.GetLogChannelPeer(ctx, ctx2.Raw, ctx2.PeerStorage)
	if err != nil {
		return 0, err
	}
	res, err := withFlood(ctx, func() (tg.UpdatesClass, error) {
		return ctx2.Raw.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
			Peer: &tg.InputPeerChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, Message: ".", RandomID: rand.Int63(), Silent: true,
		})
	})
	if err != nil {
		return 0, err
	}
	id := 0
	if u, ok := res.(*tg.Updates); ok {
		for _, upd := range u.Updates {
			switch v := upd.(type) {
			case *tg.UpdateMessageID:
				id = v.ID
			case *tg.UpdateNewChannelMessage:
				if mm, ok := v.Message.(*tg.Message); ok && id == 0 {
					id = mm.ID
				}
			}
		}
	}
	if id == 0 {
		return 0, errors.New("could not read the newest message id")
	}
	_, _ = withFlood(ctx, func() (*tg.MessagesAffectedMessages, error) {
		return ctx2.Raw.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{Channel: ch, ID: []int{id}})
	})
	return id, nil
}

type scanStats struct {
	scanned, mp4, added, dupes, badName int
}

func (s scanStats) String() string {
	return fmt.Sprintf("scanned %d, mp4 found %d, added %d, duplicates skipped %d, bad filename skipped %d", s.scanned, s.mp4, s.added, s.dupes, s.badName)
}

func dataCommand(ctx *ext.Context, u *ext.Update) error {
	if !owner(ctx, u) {
		return dispatcher.EndGroups
	}
	chat := u.EffectiveChat().GetID()
	if catalog.Default == nil {
		ctx.Reply(u, ext.ReplyTextString("Catalog database is not configured."), nil)
		return dispatcher.EndGroups
	}
	if !scanning.CompareAndSwap(false, true) {
		ctx.Reply(u, ext.ReplyTextString("A /data scan is already running. I will message you when it finishes."), nil)
		return dispatcher.EndGroups
	}
	disarmRemove(chat)
	probe, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	top, err := topMessageID(probe, ctx)
	if err != nil {
		scanning.Store(false)
		ctx.Reply(u, ext.ReplyTextString(adminHelp), nil)
		return dispatcher.EndGroups
	}
	from, err := catalog.Default.Checkpoint(probe, config.ValueOf.LogChannelID)
	if err != nil {
		scanning.Store(false)
		ctx.Reply(u, ext.ReplyTextString("Database unavailable. Try again later."), nil)
		return dispatcher.EndGroups
	}
	if from >= top {
		scanning.Store(false)
		ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("Nothing new. Everything up to message %d was already scanned.", from)), nil)
		return dispatcher.EndGroups
	}
	ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("Scan started: messages %d to %d. It runs in the background and I will send progress. Safe to leave.", from+1, top)), nil)
	go runScan(ctx, chat, from, top)
	return dispatcher.EndGroups
}

func runScan(ctx *ext.Context, chat int64, from, top int) {
	defer scanning.Store(false)
	bg := context.Background()
	var st scanStats
	lastReport := from
	fail := func(err error) {
		notify(ctx, chat, fmt.Sprintf("Scan stopped: %v\nProgress is saved. Send /data to continue.\nSo far: %s", err, st))
	}
	for start := from + 1; start <= top; start += scanBatch {
		end := min(start+scanBatch-1, top)
		ids := make([]tg.InputMessageClass, 0, end-start+1)
		for i := start; i <= end; i++ {
			ids = append(ids, &tg.InputMessageID{ID: i})
		}
		ch, err := utils.GetLogChannelPeer(bg, ctx.Raw, ctx.PeerStorage)
		if err != nil {
			fail(errors.New(adminHelp))
			return
		}
		res, err := withFlood(bg, func() (tg.MessagesMessagesClass, error) {
			return ctx.Raw.ChannelsGetMessages(bg, &tg.ChannelsGetMessagesRequest{Channel: ch, ID: ids})
		})
		if err != nil {
			fail(err)
			return
		}
		cm, ok := res.(*tg.MessagesChannelMessages)
		if !ok {
			fail(errors.New("unexpected Telegram response"))
			return
		}
		for _, mc := range cm.Messages {
			msg, ok := mc.(*tg.Message)
			if !ok {
				continue
			}
			st.scanned++
			if _, isDoc := msg.Media.(*tg.MessageMediaDocument); !isDoc {
				continue
			}
			file, err := utils.FileFromMedia(msg.Media)
			if err != nil {
				continue
			}
			slug, isMP4, valid := catalog.SlugFromFilename(file.FileName)
			if !isMP4 {
				continue
			}
			st.mp4++
			if !valid {
				st.badName++
				continue
			}
			entry := catalog.Entry{Slug: slug, ChannelID: config.ValueOf.LogChannelID, MessageID: msg.ID, Size: file.FileSize, Hash: utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID)}
			c, cancel := context.WithTimeout(bg, 15*time.Second)
			added, err := catalog.Default.AddIfNew(c, entry)
			cancel()
			if err != nil {
				fail(fmt.Errorf("database error: %w", err))
				return
			}
			if added {
				st.added++
			} else {
				st.dupes++
			}
		}
		c, cancel := context.WithTimeout(bg, 15*time.Second)
		err = catalog.Default.SetCheckpoint(c, config.ValueOf.LogChannelID, end)
		cancel()
		if err != nil {
			fail(fmt.Errorf("could not save checkpoint: %w", err))
			return
		}
		if end-lastReport >= progressEvery && end < top {
			lastReport = end
			notify(ctx, chat, fmt.Sprintf("Progress %d/%d: %s", end, top, st))
		}
		time.Sleep(scanPause)
	}
	notify(ctx, chat, fmt.Sprintf("Scan finished up to message %d: %s", top, st))
}

func disarmRemove(chat int64) {
	removeMu.Lock()
	delete(removeArmed, chat)
	removeMu.Unlock()
}

func removeCommand(ctx *ext.Context, u *ext.Update) error {
	if !owner(ctx, u) {
		return dispatcher.EndGroups
	}
	chat := u.EffectiveChat().GetID()
	args := strings.Fields(u.EffectiveMessage.Text)
	if catalog.Default == nil {
		ctx.Reply(u, ext.ReplyTextString("Catalog database is not configured."), nil)
		return dispatcher.EndGroups
	}
	if scanning.Load() {
		ctx.Reply(u, ext.ReplyTextString("A /data scan is running. Wait for it to finish before /remove."), nil)
		return dispatcher.EndGroups
	}
	req, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if len(args) == 1 {
		n, err := catalog.Default.Count(req)
		if err != nil {
			ctx.Reply(u, ext.ReplyTextString("Database unavailable. Try again later."), nil)
			return dispatcher.EndGroups
		}
		removeMu.Lock()
		removeArmed[chat] = time.Now().Add(removeConfirmTTL)
		removeMu.Unlock()
		ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("This deletes ALL %d catalog entries (every slug) and resets the /data scan position. Telegram files are not touched. Slugs stop working until you run /data again.\nTo continue, send exactly:\n/remove %s\nwithin 60 seconds. Anything else cancels.", n, removeWord)), nil)
		return dispatcher.EndGroups
	}
	removeMu.Lock()
	exp, armed := removeArmed[chat]
	delete(removeArmed, chat)
	removeMu.Unlock()
	if len(args) != 2 || args[1] != removeWord || !armed || time.Now().After(exp) {
		ctx.Reply(u, ext.ReplyTextString("Cancelled. Nothing was deleted."), nil)
		return dispatcher.EndGroups
	}
	n, err := catalog.Default.RemoveAll(req)
	if err != nil {
		ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("Failed part way (%d entries deleted). Send /remove again to retry.", n)), nil)
		return dispatcher.EndGroups
	}
	ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("Deleted %d catalog entries and reset the /data scan position. Send /data to import again.", n)), nil)
	return dispatcher.EndGroups
}
