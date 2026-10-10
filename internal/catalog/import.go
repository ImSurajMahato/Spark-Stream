// Spark Stream additions. AGPL-3.0, see LICENSE and NOTICE.
package catalog

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SlugFromFilename returns the filename without its .mp4 extension. It does no
// other normalization. ok is false when the name does not end in .mp4 or the
// result is not a valid slug.
func SlugFromFilename(name string) (slug string, isMP4 bool, valid bool) {
	if len(name) < 4 || !strings.EqualFold(name[len(name)-4:], ".mp4") {
		return "", false, false
	}
	slug = name[:len(name)-4]
	return slug, true, ValidSlug(slug)
}

// AddIfNew inserts e unless its slug or its channel+message already exist.
// It never changes an existing record. Caption is never part of Entry.
func (s *Store) AddIfNew(ctx context.Context, e Entry) (bool, error) {
	if !e.Valid() {
		return false, errors.New("invalid catalog record")
	}
	n, err := s.collection.CountDocuments(ctx, bson.M{"channel_id": e.ChannelID, "message_id": e.MessageID}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	e.CreatedAt = time.Now().UTC()
	_, err = s.collection.InsertOne(ctx, e)
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) meta() *mongo.Collection { return s.collection.Database().Collection("meta") }

type checkpoint struct {
	ID     string `bson:"_id"`
	LastID int    `bson:"last_id"`
}

func checkpointKey(channel int64) string { return "data_scan:" + itoa(channel) }

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Checkpoint returns the highest log-channel message id already scanned by /data.
func (s *Store) Checkpoint(ctx context.Context, channel int64) (int, error) {
	var c checkpoint
	err := s.meta().FindOne(ctx, bson.M{"_id": checkpointKey(channel)}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	return c.LastID, err
}

// SetCheckpoint only ever moves the checkpoint forward, so two services
// scanning the same channel cannot move it back.
func (s *Store) SetCheckpoint(ctx context.Context, channel int64, last int) error {
	_, err := s.meta().UpdateOne(ctx, bson.M{"_id": checkpointKey(channel)}, bson.M{"$max": bson.M{"last_id": last}}, options.UpdateOne().SetUpsert(true))
	return err
}

// RemoveAll deletes every catalog entry and every /data checkpoint. It returns
// how many catalog entries were deleted.
func (s *Store) RemoveAll(ctx context.Context) (int64, error) {
	res, err := s.collection.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	if _, err = s.meta().DeleteMany(ctx, bson.M{"_id": bson.M{"$regex": "^data_scan:"}}); err != nil {
		return res.DeletedCount, err
	}
	return res.DeletedCount, nil
}
