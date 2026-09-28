//
// Copyright (c) 2017-2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// What a device may stream in one batch. The contract has the device send a
// batch every 500 ms or at 32 KiB and cut lines at 8 KiB; these caps leave
// room for that and bound what one publish can make the Hub store.
const (
	// MaxLogBatchSize caps the payload of one batch; a larger one is dropped.
	MaxLogBatchSize = 128 * 1024

	// maxLogBatchLines caps the lines of one batch; the lines past it are
	// counted in dropped.
	maxLogBatchLines = 4096

	// maxLogLineLength is the device's cut (8 KiB) plus its marker; a longer
	// line is cut here.
	maxLogLineLength = 8*1024 + 64

	maxLogReasonLength = 256

	// maxLogQuotedLength caps a device-chosen string quoted in an error: the
	// bridge logs those errors, and a batch is up to MaxLogBatchSize.
	maxLogQuotedLength = 64

	// What a batch counts against MaxLogSessionBytes past its text: each
	// line costs logLineOverhead more, as the device's own rate limit
	// charges it (pv-mqttsdk pkg/logs), and each batch logBatchOverhead. So
	// a batch of empty lines is not free, while a device honouring the
	// contract (64 KiB/s of that same cost, for at most 30 minutes, plus
	// its burst, at most about 3,700 batches) stays under the budget.
	logLineOverhead  = 16
	logBatchOverhead = 256
)

// logTruncatedMarker ends a line cut by the Hub, as the device marks its own.
const logTruncatedMarker = "… [truncated]"

// Reasons a batch is not stored. The bridge drops such batches quietly.
// ErrLogSessionOverBudget is a batch dropped because its session already
// stored MaxLogSessionBatches or MaxLogSessionBytes.
var ErrLogSessionOverBudget = errors.New("log session over its storage budget")

var (
	ErrLogBatchInvalid  = errors.New("invalid log batch")
	ErrLogSessionNoLive = errors.New("no live log session")
)

// logBatchWire is a batch as a device publishes it on logs/stream.
type logBatchWire struct {
	Session string    `json:"session"`
	Seq     int64     `json:"seq"`
	Lines   []LogLine `json:"lines"`
	Dropped int64     `json:"dropped"`
	End     bool      `json:"end"`
	Reason  string    `json:"reason"`
}

// DecodeLogBatch validates a logs/stream payload and returns the batch to
// store, its lines and reason capped. Session and seq must be usable; the
// rest is capped rather than refused, so a device's final batch is not lost
// over an overlong line.
func DecodeLogBatch(payload []byte) (LogBatch, error) {
	if len(payload) > MaxLogBatchSize {
		return LogBatch{}, fmt.Errorf("%w: %d bytes", ErrLogBatchInvalid, len(payload))
	}

	wire := logBatchWire{}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return LogBatch{}, fmt.Errorf("%w: %v", ErrLogBatchInvalid, err)
	}
	session, err := primitive.ObjectIDFromHex(wire.Session)
	if err != nil {
		return LogBatch{}, fmt.Errorf("%w: session %q (%d bytes)", ErrLogBatchInvalid,
			capText(wire.Session, maxLogQuotedLength, "…"), len(wire.Session))
	}
	if wire.Seq < 0 {
		return LogBatch{}, fmt.Errorf("%w: seq %d", ErrLogBatchInvalid, wire.Seq)
	}

	dropped := wire.Dropped
	if dropped < 0 {
		dropped = 0
	}
	lines := wire.Lines
	if len(lines) > maxLogBatchLines {
		dropped += int64(len(lines) - maxLogBatchLines)
		lines = lines[:maxLogBatchLines]
	}
	if lines == nil {
		lines = []LogLine{}
	}
	for i := range lines {
		lines[i].Src = capText(lines[i].Src, maxLogSourceLength, "")
		lines[i].Line = capText(lines[i].Line, maxLogLineLength, logTruncatedMarker)
	}

	return LogBatch{
		Session: session,
		Seq:     wire.Seq,
		Lines:   lines,
		Dropped: dropped,
		End:     wire.End,
		Reason:  capText(wire.Reason, maxLogReasonLength, ""),
	}, nil
}

// capText cuts s to at most max bytes without splitting a character, and
// appends marker when it did.
func capText(s string, max int, marker string) string {
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + marker
}

// logSessionAccepts reports whether a session still takes batches at now:
// before its lease and deadline, and not ended, give or take LogSessionGrace
// for the device's last batch.
func logSessionAccepts(session *LogSession, now time.Time) bool {
	end := session.ExpiresAt
	if session.Deadline.Before(end) {
		end = session.Deadline
	}
	if !now.Before(end.Add(LogSessionGrace)) {
		return false
	}
	if !session.Live && (session.EndedAt == nil || !now.Before(session.EndedAt.Add(LogSessionGrace))) {
		return false
	}
	return true
}

// StoreLogBatch stores a batch deviceID streamed, for a session of that device
// that still takes batches, and ends the session when the batch is its last.
// A batch for an unknown session, another device's, or one that is over is
// ErrLogSessionNoLive. A batch stored already (a redelivery) is not an error.
func StoreLogBatch(ctx context.Context, client *mongo.Client, deviceID primitive.ObjectID, batch LogBatch, now time.Time) error {
	db := client.Database(utils.MongoDb)

	session := LogSession{}
	err := db.Collection(LogSessionsCollection).FindOne(ctx,
		bson.M{"_id": batch.Session, "device_id": deviceID},
		options.FindOne().SetProjection(bson.M{"live": 1, "expires_at": 1, "deadline": 1, "ended_at": 1})).Decode(&session)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("%w: %s for device %s", ErrLogSessionNoLive, batch.Session.Hex(), deviceID.Hex())
	}
	if err != nil {
		return err
	}
	if !logSessionAccepts(&session, now) {
		return fmt.Errorf("%w: %s is over", ErrLogSessionNoLive, batch.Session.Hex())
	}

	// The session's budget is taken before the batch is stored, in one
	// conditional update: a device flooding its own session stops being
	// stored at MaxLogSessionBatches / MaxLogSessionBytes, whatever the
	// replica its batches land on. Its last batch still ends the session.
	res, err := db.Collection(LogSessionsCollection).UpdateOne(ctx,
		bson.M{
			"_id":            batch.Session,
			"device_id":      deviceID,
			"stream_batches": bson.M{"$not": bson.M{"$gte": MaxLogSessionBatches}},
			"stream_bytes":   bson.M{"$not": bson.M{"$gte": MaxLogSessionBytes}},
		},
		bson.M{"$inc": bson.M{"stream_batches": 1, "stream_bytes": logBatchBytes(batch)}})
	if err != nil {
		return err
	}
	overBudget := res.MatchedCount == 0
	if !overBudget {
		batch.ID = primitive.NewObjectID()
		batch.DeviceID = deviceID
		batch.CreatedAt = now
		if _, err := db.Collection(LogStreamCollection).InsertOne(ctx, batch); err != nil && !mongo.IsDuplicateKeyError(err) {
			return err
		}
	}

	if batch.End && session.Live {
		reason := batch.Reason
		if reason == "" {
			reason = "ended"
		}
		_, err := db.Collection(LogSessionsCollection).UpdateOne(ctx,
			bson.M{"_id": batch.Session, "device_id": deviceID, "live": true},
			bson.M{"$set": bson.M{
				"live":     false,
				"ended_at": now,
				"ended_by": LogEndedByDevice,
				"reason":   reason,
			}})
		if err != nil {
			return err
		}
	}
	if overBudget {
		return fmt.Errorf("%w: %s", ErrLogSessionOverBudget, batch.Session.Hex())
	}
	return nil
}

// logBatchBytes is what a batch counts against MaxLogSessionBytes: its lines
// and their sources, logLineOverhead per line and logBatchOverhead.
func logBatchBytes(batch LogBatch) int64 {
	n := int64(logBatchOverhead)
	for _, l := range batch.Lines {
		n += int64(len(l.Line) + len(l.Src) + logLineOverhead)
	}
	return n
}
