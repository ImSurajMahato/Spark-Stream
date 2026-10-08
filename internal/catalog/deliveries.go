package catalog

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const FileLifetime = 4 * time.Hour

// A durable intent is written BEFORE sending. RandomID is reused on recovery,
// so Telegram can deduplicate a send interrupted before its result was saved.
// Do not TTL-delete these records: MongoDB expiry does not delete Telegram messages.
type Delivery struct {
	Hash        string    `bson:"hash"`
	ID          string    `bson:"_id"`
	UserID      int64     `bson:"user_id"`
	AccessHash  int64     `bson:"access_hash"`
	SourceID    int       `bson:"source_id"`
	ChannelID   int64     `bson:"channel_id"`
	RandomID    int64     `bson:"random_id"`
	MessageID   int       `bson:"message_id"`
	CreatedAt   time.Time `bson:"created_at"`
	DeleteAt    time.Time `bson:"delete_at"`
	NextAttempt time.Time `bson:"next_attempt"`
	State       string    `bson:"state"`
	Attempts    int       `bson:"attempts"`
	Failure     string    `bson:"failure,omitempty"`
}

func (d Delivery) Valid() bool {
	return ValidSlug(d.ID) && d.UserID > 0 && d.SourceID > 0 && d.ChannelID > 0 && d.RandomID != 0 && len(d.Hash) == 32 && d.MessageID >= 0 && !d.CreatedAt.IsZero() && d.DeleteAt.Equal(d.CreatedAt.Add(FileLifetime)) && (d.State == "sending" || d.State == "sent" || d.State == "failed") && (d.State != "sent" || d.MessageID > 0)
}
func NewDelivery(id string, user, access, channel, random int64, source int, hash string, now time.Time) Delivery {
	// BSON dates have millisecond precision. Normalize before comparisons/readback.
	now = now.UTC().Truncate(time.Millisecond)
	return Delivery{ID: id, Hash: hash, UserID: user, AccessHash: access, ChannelID: channel, SourceID: source, RandomID: random, CreatedAt: now, DeleteAt: now.Add(FileLifetime), NextAttempt: now.Add(time.Minute), State: "sending"}
}
func (s *Store) deliveries() *mongo.Collection {
	return s.collection.Database().Collection("file_deliveries")
}
func (s *Store) InitDeliveries(ctx context.Context) error {
	_, err := s.deliveries().Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "state", Value: 1}, {Key: "next_attempt", Value: 1}}})
	return err
}
func (s *Store) QueueDelivery(ctx context.Context, d Delivery) error {
	if !d.Valid() || d.State != "sending" || d.MessageID != 0 {
		return errors.New("invalid delivery intent")
	}
	_, err := s.deliveries().InsertOne(ctx, d)
	return err
}
func (s *Store) DeliverySent(ctx context.Context, d Delivery, messageID int) error {
	if messageID <= 0 {
		return errors.New("missing delivered message ID")
	}
	r, err := s.deliveries().UpdateOne(ctx, bson.M{"_id": d.ID, "state": "sending"}, bson.M{"$set": bson.M{"message_id": messageID, "state": "sent", "next_attempt": d.DeleteAt}})
	if err == nil && r.MatchedCount != 1 {
		return errors.New("delivery intent unavailable")
	}
	return err
}
func (s *Store) DueDeliveries(ctx context.Context, now time.Time) ([]Delivery, error) {
	cur, err := s.deliveries().Find(ctx, bson.M{"state": bson.M{"$in": []string{"sending", "sent"}}, "next_attempt": bson.M{"$lte": now}}, options.Find().SetSort(bson.D{{Key: "next_attempt", Value: 1}}).SetLimit(50))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var ds []Delivery
	if err = cur.All(ctx, &ds); err != nil {
		return nil, err
	}
	for _, d := range ds {
		if !d.Valid() {
			return nil, errors.New("invalid deletion record; inspect file_deliveries")
		}
	}
	return ds, nil
}
func (s *Store) FinishDelivery(ctx context.Context, id string) error {
	_, err := s.deliveries().DeleteOne(ctx, bson.M{"_id": id})
	return err
}
func RetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Minute * time.Duration(1<<attempt)
}
func (s *Store) RetryDelivery(ctx context.Context, d Delivery, delay time.Duration, terminal bool, reason string) error {
	state := d.State
	if terminal {
		state = "failed"
	}
	_, err := s.deliveries().UpdateOne(ctx, bson.M{"_id": d.ID}, bson.M{"$set": bson.M{"state": state, "next_attempt": time.Now().UTC().Add(delay), "failure": reason}, "$inc": bson.M{"attempts": 1}})
	return err
}
