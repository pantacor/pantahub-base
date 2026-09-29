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
	"log"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// MaxSSHFramePayload caps the JSON of one frame: MaxSSHFrameData in
	// base64 and room for the other fields.
	MaxSSHFramePayload = (MaxSSHFrameData+2)/3*4 + 1024

	maxSSHReasonLength = 256
)

var (
	ErrSSHFrameInvalid      = errors.New("invalid ssh frame")
	ErrSSHSessionNoLive     = errors.New("no live ssh session")
	ErrSSHSessionOverRate   = errors.New("ssh session above its rate")
	ErrSSHSessionUnattached = errors.New("ssh session streamed without a client")
	errSSHSessionNotStored  = errors.New("ssh frame not stored")
)

// SSHFrame is a frame as stored in SSHFramesCollection.
type SSHFrame struct {
	ID        primitive.ObjectID `bson:"_id"`
	Session   primitive.ObjectID `bson:"session"`
	DeviceID  primitive.ObjectID `bson:"device_id"`
	Dir       string             `bson:"dir"`
	Seq       int64              `bson:"seq"`
	Data      []byte             `bson:"data"`
	End       bool               `bson:"end"`
	Reason    string             `bson:"reason"`
	CreatedAt time.Time          `bson:"created_at"`
}

// sshFrameWire is a frame on ph/v1/dev/<id>/ssh/<sid>/{up,down}:
//
//	{"seq": 12, "data": "<base64 bytes>", "end": false, "reason": ""}
//
// encoding/json carries []byte as standard base64.
type sshFrameWire struct {
	Seq    *int64 `json:"seq"`
	Data   []byte `json:"data"`
	End    bool   `json:"end"`
	Reason string `json:"reason"`
}

// DecodeSSHFrame validates a down frame's payload: a seq of 0 or more, at most
// MaxSSHFrameData bytes of data; a long reason is cut short.
func DecodeSSHFrame(payload []byte) (SSHFrame, error) {
	if len(payload) > MaxSSHFramePayload {
		return SSHFrame{}, fmt.Errorf("%w: %d bytes", ErrSSHFrameInvalid, len(payload))
	}
	wire := sshFrameWire{}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return SSHFrame{}, fmt.Errorf("%w: %v", ErrSSHFrameInvalid, err)
	}
	if wire.Seq == nil || *wire.Seq < 0 {
		return SSHFrame{}, fmt.Errorf("%w: no seq, or a negative one", ErrSSHFrameInvalid)
	}
	if len(wire.Data) > MaxSSHFrameData {
		return SSHFrame{}, fmt.Errorf("%w: %d bytes of data", ErrSSHFrameInvalid, len(wire.Data))
	}
	if wire.Data == nil {
		wire.Data = []byte{}
	}
	return SSHFrame{
		Seq:    *wire.Seq,
		Data:   wire.Data,
		End:    wire.End,
		Reason: capText(wire.Reason, maxSSHReasonLength, ""),
	}, nil
}

// EncodeSSHFrame is the payload of a frame as published.
func EncodeSSHFrame(frame *SSHFrame) ([]byte, error) {
	data := frame.Data
	if data == nil {
		data = []byte{}
	}
	seq := frame.Seq
	return json.Marshal(sshFrameWire{Seq: &seq, Data: data, End: frame.End, Reason: frame.Reason})
}

// sshSessionAccepts reports whether a session still takes the device's frames
// at now: before its lease and deadline, and not ended, give or take
// SSHSessionGrace for the device's last frame.
func sshSessionAccepts(session *SSHSession, now time.Time) bool {
	end := session.ExpiresAt
	if session.Deadline.Before(end) {
		end = session.Deadline
	}
	if !now.Before(end.Add(SSHSessionGrace)) {
		return false
	}
	if !session.Live && (session.EndedAt == nil || !now.Before(session.EndedAt.Add(SSHSessionGrace))) {
		return false
	}
	return true
}

func loadSSHSessionForDevice(ctx context.Context, client *mongo.Client, deviceID, sessionID primitive.ObjectID) (*SSHSession, error) {
	session := SSHSession{}
	err := client.Database(utils.MongoDb).Collection(SSHSessionsCollection).FindOne(ctx,
		bson.M{"_id": sessionID, "device_id": deviceID},
		options.FindOne().SetProjection(bson.M{
			"live": 1, "expires_at": 1, "deadline": 1, "ended_at": 1, "attached": 1, "unattached_bytes": 1,
		})).Decode(&session)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// sshDownRateUpdate charges a down frame of n bytes to its session: the
// window of sshRateWindow starts again once over, the totals grow, the
// bytes taken without a WebSocket attached add up (and go back to zero once
// one is), and a frame with data marks the session active. The filter that
// goes with it (sshDownRateFilter) only matches while the window and the
// unattached allowance have room.
func sshDownRateUpdate(n int64, now time.Time) mongo.Pipeline {
	reset := bson.M{"$lte": bson.A{"$down_window_at", now.Add(-sshRateWindow)}}
	active := interface{}("$active_at")
	if n > 0 {
		active = now
	}
	// A session from before the counter existed has no field: it counts as 0.
	unattached := bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$unattached_bytes", 0}}, n}}
	return mongo.Pipeline{{{Key: "$set", Value: bson.M{
		"down_window_at":     bson.M{"$cond": bson.A{reset, now, "$down_window_at"}},
		"down_window_bytes":  bson.M{"$cond": bson.A{reset, n, bson.M{"$add": bson.A{"$down_window_bytes", n}}}},
		"down_window_frames": bson.M{"$cond": bson.A{reset, 1, bson.M{"$add": bson.A{"$down_window_frames", 1}}}},
		"down_bytes":         bson.M{"$add": bson.A{"$down_bytes", n}},
		"down_frames":        bson.M{"$add": bson.A{"$down_frames", 1}},
		"unattached_bytes":   bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$attached", true}}, 0, unattached}},
		"active_at":          active,
	}}}}
}

// sshDownRateFilter matches the session while it has room for a frame of n
// bytes: in its rate window, and, without a WebSocket attached, in
// SSHUnattachedBytes. A missing counter (a session from before it existed)
// is 0, which is what "$not $gt" gives.
func sshDownRateFilter(sessionID, deviceID primitive.ObjectID, n int64, now time.Time) bson.M {
	return bson.M{
		"_id":       sessionID,
		"device_id": deviceID,
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"down_window_at": bson.M{"$lte": now.Add(-sshRateWindow)}},
				bson.M{
					"down_window_bytes":  bson.M{"$lte": sshRateWindowBytes - n},
					"down_window_frames": bson.M{"$lt": sshRateWindowFrames},
				},
			}},
			bson.M{"$or": bson.A{
				bson.M{"attached": true},
				bson.M{"unattached_bytes": bson.M{"$not": bson.M{"$gt": SSHUnattachedBytes - n}}},
			}},
		},
	}
}

// sshDownOverUnattached reports whether a frame of n bytes was refused for
// the unattached allowance rather than the rate: session is the document as
// read just before the refused update (a device's frames arrive one at a
// time on its connection, so it is what the update saw).
func sshDownOverUnattached(session *SSHSession, n int64) bool {
	return !session.Attached && session.UnattachedBytes+n > SSHUnattachedBytes
}

// StoreSSHDownFrame stores a frame deviceID published for one of its sessions
// that still takes frames, and ends the session when the frame is its last.
// A frame for an unknown session, another device's, or one that is over is
// ErrSSHSessionNoLive. A device above the rate (SSHRate, checked over
// sshRateWindow) has its session ended, and the frame is
// ErrSSHSessionOverRate: dropping a frame would break the SSH stream anyway.
// So has a device that streams more than SSHUnattachedBytes while no
// WebSocket is attached to take the frames (ErrSSHSessionUnattached): the
// SSH banner and key exchange fit in the allowance, and nothing else is
// worth storing for a client that is not there. A frame stored already (a
// redelivery) is not an error.
func StoreSSHDownFrame(ctx context.Context, client *mongo.Client, deviceID, sessionID primitive.ObjectID, frame SSHFrame, now time.Time) error {
	db := client.Database(utils.MongoDb)

	session, err := loadSSHSessionForDevice(ctx, client, deviceID, sessionID)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("%w: %s for device %s", ErrSSHSessionNoLive, sessionID.Hex(), deviceID.Hex())
	}
	if err != nil {
		return err
	}
	if !sshSessionAccepts(session, now) {
		return fmt.Errorf("%w: %s is over", ErrSSHSessionNoLive, sessionID.Hex())
	}

	n := int64(len(frame.Data))
	res, err := db.Collection(SSHSessionsCollection).UpdateOne(ctx,
		sshDownRateFilter(sessionID, deviceID, n, now), sshDownRateUpdate(n, now))
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		reason, refused := SSHEndRate, ErrSSHSessionOverRate
		if sshDownOverUnattached(session, n) {
			reason, refused = SSHEndUnattached, ErrSSHSessionUnattached
		}
		if _, err := EndSSHSession(ctx, client, sessionID, deviceID, SSHEndedByHub, reason, now); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", refused, sessionID.Hex())
	}

	frame.ID = primitive.NewObjectID()
	frame.Session = sessionID
	frame.DeviceID = deviceID
	frame.Dir = SSHDirDown
	frame.CreatedAt = now
	if _, err := db.Collection(SSHFramesCollection).InsertOne(ctx, frame); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}

	if frame.End && session.Live {
		reason := frame.Reason
		if reason == "" {
			reason = SSHEndClosed
		}
		if _, err := EndSSHSession(ctx, client, sessionID, deviceID, SSHEndedByDevice, reason, now); err != nil {
			return err
		}
	}
	return nil
}

// storeSSHUpFrame stores a frame of the browser's bytes for a live session,
// counting it; a session that has ended is ErrSSHSessionNoLive.
func storeSSHUpFrame(ctx context.Context, client *mongo.Client, session *SSHSession, seq int64, data []byte, now time.Time) error {
	db := client.Database(utils.MongoDb)
	update := bson.M{"$inc": bson.M{"up_bytes": len(data), "up_frames": 1}}
	if len(data) > 0 {
		update["$set"] = bson.M{"active_at": now}
	}
	res, err := db.Collection(SSHSessionsCollection).UpdateOne(ctx, bson.M{"_id": session.ID, "live": true}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("%w: %s", ErrSSHSessionNoLive, session.ID.Hex())
	}

	frame := SSHFrame{
		ID: primitive.NewObjectID(), Session: session.ID, DeviceID: session.DeviceID, Dir: SSHDirUp,
		Seq: seq, Data: data, CreatedAt: now,
	}
	if _, err := db.Collection(SSHFramesCollection).InsertOne(ctx, frame); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// Only one WebSocket serves a session, from seq 0 (the claim
			// is atomic): a frame already there, not yet forwarded and
			// deleted, means a second writer, which must stop.
			log.Printf("devices: ssh session %s: up frame %d stored twice", session.ID.Hex(), seq)
			return fmt.Errorf("%w: up frame %d exists", errSSHSessionNotStored, seq)
		}
		return err
	}
	return nil
}
