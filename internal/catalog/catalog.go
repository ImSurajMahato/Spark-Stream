// Spark Stream additions. AGPL-3.0, see LICENSE and NOTICE.
package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var ErrConflict = errors.New("slug already belongs to another file")
var slugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ValidSlug(s string) bool { return slugPattern.MatchString(s) }
func RandomSlug() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

type Entry struct {
	Slug      string    `bson:"_id"`
	ChannelID int64     `bson:"channel_id"`
	MessageID int       `bson:"message_id"`
	Hash      string    `bson:"hash"`
	CreatedAt time.Time `bson:"created_at"`
}

func (e Entry) Valid() bool {
	if !ValidSlug(e.Slug) || e.ChannelID <= 0 || e.MessageID <= 0 || len(e.Hash) != 32 {
		return false
	}
	_, err := hex.DecodeString(e.Hash)
	return err == nil
}

type Store struct {
	client     *mongo.Client
	collection *mongo.Collection
}

var Default *Store

func Open(ctx context.Context, uri, database string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetMaxPoolSize(10).SetMinPoolSize(0).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		return nil, err
	}
	if err = client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return &Store{client, client.Database(database).Collection("lectures")}, nil
}
func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }
func (s *Store) Get(ctx context.Context, slug string) (Entry, error) {
	var e Entry
	if !ValidSlug(slug) {
		return e, errors.New("invalid slug")
	}
	err := s.collection.FindOne(ctx, bson.M{"_id": slug}).Decode(&e)
	if err == nil && !e.Valid() {
		err = errors.New("invalid catalog record")
	}
	return e, err
}

// Atomic unique _id insert. Idempotent same-file retries are safe, never rebind a public slug.
func (s *Store) Add(ctx context.Context, e Entry) error {
	if !e.Valid() {
		return errors.New("invalid catalog record")
	}
	e.CreatedAt = time.Now().UTC()
	_, err := s.collection.InsertOne(ctx, e)
	if mongo.IsDuplicateKeyError(err) {
		old, getErr := s.Get(ctx, e.Slug)
		if getErr != nil {
			return getErr
		}
		if old.ChannelID == e.ChannelID && old.MessageID == e.MessageID && old.Hash == e.Hash {
			return nil
		}
		return ErrConflict
	}
	return err
}
