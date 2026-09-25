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

package mqtt

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Presence: the device-meta keys under devices.DeviceMetaMqttPrefix record
// whether a device holds an MQTT connection commands can be delivered on.
//
// They follow the broker's own view of the connection rather than what the
// device publishes on its status topic, because with several API replicas
// behind one ingress the reports of two connections of the same device race
// each other: a device that reconnects to replica B is online there, and
// replica A only notices the old socket died up to 1.5x keepalive later. So:
//
//   - every connection gets an id, "<broker id>/<nonce>", and records itself
//     as the device's current connection once established;
//   - it marks the device connected once it is subscribed to its commands
//     topic (before that a command would be lost), and its disconnect clears
//     the flag, both only while it is still the current connection: the late
//     disconnect (or will, see bridgeHook.OnWillSent) of an older connection
//     matches nothing;
//   - all of these writes are synchronous on the connection's own goroutine,
//     so they land in the order the connection went through them;
//   - each replica refreshes a heartbeat, and the flag is only believed while
//     the heartbeat of the replica that wrote it is fresh: a replica that is
//     killed never runs its disconnects.

const (
	// presenceTimeout bounds the connection-state writes. They run on the
	// connection's goroutine, so a stalled database delays that one client
	// only.
	presenceTimeout = 5 * time.Second

	// Per-connection user properties, next to the identity (see auth.go):
	// the connection id, and whether this connection marked the device
	// connected.
	propConnection = "ph-connection"
	propConnected  = "ph-connected"
)

// newBrokerID names this broker replica. It is unique per process, so a pod
// restarted under the same name is a different broker: the connections of
// the previous process are not vouched for by the new heartbeat.
func newBrokerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "broker"
	}
	return host + "-" + primitive.NewObjectID().Hex()
}

// presenceHook keeps the MQTT connection state of devices in device-meta.
type presenceHook struct {
	mochi.HookBase
	mongoClient *mongo.Client
	server      *mochi.Server
	brokerID    string
}

// ID identifies the hook in broker logs.
func (h *presenceHook) ID() string {
	return "pantahub-presence"
}

// Provides declares the hook points this hook implements.
func (h *presenceHook) Provides(b byte) bool {
	switch b {
	case mochi.OnSessionEstablish, mochi.OnSessionEstablished, mochi.OnSubscribed, mochi.OnDisconnect:
		return true
	}
	return false
}

// OnSessionEstablish runs after authentication and before the broker takes
// over an existing session with the same client id.
//
// The session being taken over is disarmed of its will first. mochi
// disconnects it during the takeover and only then flags it as taken over, so
// its goroutine can send the will before the flag is visible (and the will is
// published and retained whatever a hook does with it): the device would be
// reported offline right as it reconnected. Clearing the flag here, before the
// takeover starts, means the will is never sent.
func (h *presenceHook) OnSessionEstablish(cl *mochi.Client, pk packets.Packet) {
	kind, _ := identity(cl)
	if kind != kindDevice {
		return
	}

	if h.server != nil {
		if existing, ok := h.server.Clients.Get(cl.ID); ok && existing != cl {
			atomic.StoreUint32(&existing.Properties.Will.Flag, 0)
		}
	}

	setClientProp(cl, propConnection, h.brokerID+"/"+primitive.NewObjectID().Hex())
}

// OnSessionEstablished records the connection as the device's current one. It
// is connected for commands right away when the session it resumed already
// carries the commands subscription (a persistent session taken over on this
// replica), otherwise once it subscribes (OnSubscribed). It runs before the
// broker reads anything from the client.
func (h *presenceHook) OnSessionEstablished(cl *mochi.Client, pk packets.Packet) {
	kind, deviceID := identity(cl)
	if kind != kindDevice {
		return
	}

	_, subscribed := cl.State.Subscriptions.Get(Topic(deviceID, SuffixCommands))
	if err := h.writeEstablished(deviceID, clientProp(cl, propConnection), subscribed); err != nil {
		log.Printf("mqtt: presence: cannot record connection of %s: %v", deviceID, err)
		return
	}
	if subscribed {
		setClientProp(cl, propConnected, "1")
	}
}

// OnSubscribed marks the device connected once its commands subscription is
// granted. It runs on the client's goroutine, before the SUBACK is written.
func (h *presenceHook) OnSubscribed(cl *mochi.Client, pk packets.Packet, reasonCodes []byte) {
	kind, deviceID := identity(cl)
	if kind != kindDevice || clientProp(cl, propConnected) != "" {
		return
	}

	commands := Topic(deviceID, SuffixCommands)
	for i, sub := range pk.Filters {
		if sub.Filter != commands || i >= len(reasonCodes) || reasonCodes[i] >= packets.ErrUnspecifiedError.Code {
			continue
		}
		if err := h.writeConnected(deviceID, clientProp(cl, propConnection), true); err != nil {
			log.Printf("mqtt: presence: cannot record commands subscription of %s: %v", deviceID, err)
			return
		}
		setClientProp(cl, propConnected, "1")
		return
	}
}

// OnDisconnect clears the connected flag, if it still belongs to this
// connection. It runs on the client's goroutine after the read loop ended, so
// after every other write of the same connection.
func (h *presenceHook) OnDisconnect(cl *mochi.Client, err error, expire bool) {
	kind, deviceID := identity(cl)
	if kind != kindDevice || clientProp(cl, propConnected) == "" {
		return
	}

	if err := h.writeConnected(deviceID, clientProp(cl, propConnection), false); err != nil {
		log.Printf("mqtt: presence: cannot record disconnect of %s: %v", deviceID, err)
	}
}

// writeEstablished makes connection the device's current one, on this broker.
func (h *presenceHook) writeEstablished(deviceID, connection string, connected bool) error {
	if h.mongoClient == nil || connection == "" {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), presenceTimeout)
	defer cancel()

	result, err := h.mongoClient.Database(utils.MongoDb).Collection(devicesCollection).UpdateOne(ctx,
		bson.M{"_id": id, "garbage": bson.M{"$ne": true}},
		presenceUpdate(bson.M{
			presenceKey(devices.DeviceMetaMqttConnected):  connected,
			presenceKey(devices.DeviceMetaMqttConnection): connection,
			presenceKey(devices.DeviceMetaMqttBroker):     h.brokerID,
		}),
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("device %s not found", deviceID)
	}
	return nil
}

// writeConnected sets the connected flag while connection is still the
// device's current one; otherwise it is a no-op.
func (h *presenceHook) writeConnected(deviceID, connection string, connected bool) error {
	if h.mongoClient == nil || connection == "" {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), presenceTimeout)
	defer cancel()

	_, err = h.mongoClient.Database(utils.MongoDb).Collection(devicesCollection).UpdateOne(ctx,
		bson.M{"_id": id, presenceKey(devices.DeviceMetaMqttConnection): connection},
		presenceUpdate(bson.M{presenceKey(devices.DeviceMetaMqttConnected): connected}),
	)
	return err
}

// presenceUpdate stamps a connection-state change with its time, and bumps
// meta-modified like every device-meta write.
func presenceUpdate(set bson.M) bson.M {
	now := time.Now()
	set[presenceKey(devices.DeviceMetaMqttStatusTime)] = now.UTC().Format(time.RFC3339)
	set["meta-modified"] = now
	set["timemodified"] = now
	return bson.M{"$set": set}
}

// presenceKey is the stored path of a device-meta key.
func presenceKey(key string) string {
	return "device-meta." + utils.BsonQuote(key)
}

// connectionFilter narrows a device update to the connection that is still
// the device's current one, for writes that must not outlive it (its will).
// Empty for a client without a connection id, which leaves the update as is.
func connectionFilter(cl *mochi.Client) bson.M {
	connection := ""
	if cl != nil {
		connection = clientProp(cl, propConnection)
	}
	if connection == "" {
		return nil
	}
	return bson.M{presenceKey(devices.DeviceMetaMqttConnection): connection}
}

// runHeartbeat keeps this replica's heartbeat fresh until ctx is done.
func (h *presenceHook) runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(devices.MqttBrokerHeartbeat)
	defer ticker.Stop()

	for {
		if err := h.beat(ctx); err != nil && ctx.Err() == nil {
			log.Printf("mqtt: presence: heartbeat failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *presenceHook) beat(ctx context.Context) error {
	if h.mongoClient == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, presenceTimeout)
	defer cancel()

	_, err := h.mongoClient.Database(utils.MongoDb).Collection(devices.MqttBrokersCollection).UpdateOne(ctx,
		bson.M{"_id": h.brokerID},
		bson.M{"$set": bson.M{"heartbeat_at": time.Now()}},
		options.Update().SetUpsert(true),
	)
	return err
}

// retire removes the heartbeat: the connections of a replica that shuts down
// are gone.
func (h *presenceHook) retire() {
	if h.mongoClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), presenceTimeout)
	defer cancel()

	_, err := h.mongoClient.Database(utils.MongoDb).Collection(devices.MqttBrokersCollection).DeleteOne(ctx, bson.M{"_id": h.brokerID})
	if err != nil {
		log.Printf("mqtt: presence: cannot remove heartbeat: %v", err)
	}
}

// EnsureBrokerIndices lets MongoDB drop the heartbeats of replicas that are
// long gone.
func EnsureBrokerIndices(mongoClient *mongo.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := mongoClient.Database(utils.MongoDb).Collection(devices.MqttBrokersCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "heartbeat_at", Value: int32(1)}},
		Options: options.Index().SetExpireAfterSeconds(int32((24 * time.Hour).Seconds())),
	})
	return err
}

// clientProp and setClientProp keep per-connection values next to the identity
// in the client's user properties (see setIdentity).
func clientProp(cl *mochi.Client, key string) string {
	cl.RLock()
	defer cl.RUnlock()

	for _, prop := range cl.Properties.Props.User {
		if prop.Key == key {
			return prop.Val
		}
	}
	return ""
}

func setClientProp(cl *mochi.Client, key, value string) {
	cl.Lock()
	defer cl.Unlock()

	for i, prop := range cl.Properties.Props.User {
		if prop.Key == key {
			cl.Properties.Props.User[i].Val = value
			return
		}
	}
	cl.Properties.Props.User = append(cl.Properties.Props.User, packets.UserProperty{Key: key, Val: value})
}
