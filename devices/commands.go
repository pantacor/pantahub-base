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
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Remote device commands: an owner asks the Hub to run one of a fixed set of
// pv-ctrl calls on a device connected over MQTT. The document written here is
// delivered by the MQTT notifier (change stream on inserts) and completed by
// the MQTT bridge when the device publishes its result. The wire contract is
// docs/commands.md in pv-mqttsdk; field names and values here follow it.

// CommandsCollection holds one document per command sent to a device.
const CommandsCollection = "pantahub_device_commands"

// Command lifecycle. A command is written pending and completed by the device's
// result. CommandStatusTimeout is never stored: it is how the API reports a
// command still pending after CommandTimeout.
const (
	CommandStatusPending  = "pending"
	CommandStatusOK       = "ok"
	CommandStatusError    = "error"
	CommandStatusRejected = "rejected"
	CommandStatusTimeout  = "timeout"
)

const (
	// CommandExpiry is how long after creation a device may still execute a
	// command. It travels as expires_at, so a command held in a persistent MQTT
	// session is dropped rather than replayed long after it was sent.
	CommandExpiry = 60 * time.Second

	// CommandTimeout is how long a command may stay pending before the API
	// reports it as timed out.
	CommandTimeout = 120 * time.Second

	// CommandArgMessage is the only argument any command takes: the optional
	// REBOOT_DEVICE message handed to pv-ctrl.
	CommandArgMessage = "message"

	// maxCommandMessageLength keeps the reboot message a message. A command
	// travels in a single MQTT packet and the broker caps packet size, so an
	// oversized body is refused here rather than silently undeliverable.
	maxCommandMessageLength = 1024

	// maxCommandRequestSize caps the body of POST /devices/{id}/commands,
	// read before it is parsed: a command and its message fit in far less.
	maxCommandRequestSize = 8 * 1024

	commandsDefaultLimit = 20
	commandsMaxLimit     = 100

	// CommandRetention is how long a command is kept. Past it a TTL index
	// removes it: commands are an audit trail of recent actions, and each
	// may carry up to 256 KiB of output.
	CommandRetention = 30 * 24 * time.Hour
)

// DeviceCommands is the allowlist of commands a device may be asked to run,
// and the only one: the API refuses anything else, and the device agent
// refuses it again. The value reports whether the command takes the optional
// "message" argument; every other argument is dropped.
var DeviceCommands = map[string]bool{
	"REBOOT_DEVICE":      true,
	"RUN_GC":             false,
	"ENABLE_SSH":         false,
	"DISABLE_SSH":        false,
	"LIST_CONTAINERS":    false,
	"LIST_GROUPS":        false,
	"LIST_DAEMONS":       false,
	"LIST_DRIVERS":       false,
	"LIST_WAKELOCKS":     false,
	"GET_XCONNECT_GRAPH": false,
}

// Device-meta keys recording the device's MQTT connection. The broker writes
// them, and only the broker: device-meta a device reports is stripped of every
// key under DeviceMetaMqttPrefix (StripMqttDeviceMeta).
const (
	// DeviceMetaMqttPrefix is the namespace of the broker-written keys.
	DeviceMetaMqttPrefix = "pantahub.mqtt."

	// DeviceMetaMqttConnected is true while the device holds an MQTT
	// connection subscribed to its commands topic. Commands are only
	// accepted then.
	DeviceMetaMqttConnected = DeviceMetaMqttPrefix + "connected"

	// DeviceMetaMqttConnection identifies the connection that last changed
	// DeviceMetaMqttConnected. A disconnect only clears the flag while it
	// still names that connection, so the late disconnect of an older
	// connection (on this replica or another) cannot mark a reconnected
	// device offline.
	DeviceMetaMqttConnection = DeviceMetaMqttPrefix + "connection"

	// DeviceMetaMqttBroker is the broker replica holding that connection. The
	// flag is only believed while the replica's heartbeat in
	// MqttBrokersCollection is fresh: a replica killed without a chance to
	// disconnect its clients leaves the flag behind.
	DeviceMetaMqttBroker = DeviceMetaMqttPrefix + "broker"

	// DeviceMetaMqttStatusTime is the RFC 3339 UTC time of the last change.
	DeviceMetaMqttStatusTime = DeviceMetaMqttPrefix + "status-time"
)

// MqttBrokersCollection holds one heartbeat document per running MQTT broker
// replica: {_id: <broker id>, heartbeat_at: <time>}.
const MqttBrokersCollection = "pantahub_mqtt_brokers"

const (
	// MqttBrokerHeartbeat is how often a broker replica refreshes its
	// heartbeat.
	MqttBrokerHeartbeat = 30 * time.Second

	// MqttBrokerLease is how long a heartbeat vouches for the connections of
	// its replica: three missed beats and they are no longer believed.
	MqttBrokerLease = 3 * MqttBrokerHeartbeat
)

// StripMqttDeviceMeta removes the broker-written keys from device-meta a
// device reports, so that only the broker ever writes them. It takes the
// BSON-quoted map, as stored: a key spelled with the quoting sentinel instead
// of dots is caught as well.
func StripMqttDeviceMeta(quoted map[string]interface{}) map[string]interface{} {
	prefix := utils.BsonQuote(DeviceMetaMqttPrefix)
	for key := range quoted {
		if strings.HasPrefix(key, prefix) {
			delete(quoted, key)
		}
	}
	return quoted
}

// ReplaceDeviceMetaUpdate is the update pipeline that sets the fields in set
// and replaces device-meta with the device-reported deviceMeta (BSON-quoted),
// keeping the broker's connection keys: they are not the device's to write or
// to erase, so they are dropped from deviceMeta and carried over from the
// stored device-meta, in the same atomic update. Every value is taken
// literally, never as an expression.
func ReplaceDeviceMetaUpdate(set map[string]interface{}, deviceMeta map[string]interface{}) mongo.Pipeline {
	stage := bson.M{}
	for key, value := range set {
		stage[key] = bson.M{"$literal": value}
	}

	kept := bson.M{"$arrayToObject": bson.M{"$filter": bson.M{
		"input": bson.M{"$objectToArray": bson.M{"$ifNull": bson.A{"$device-meta", bson.M{}}}},
		"as":    "kv",
		"cond": bson.M{"$eq": bson.A{
			bson.M{"$indexOfCP": bson.A{"$$kv.k", utils.BsonQuote(DeviceMetaMqttPrefix)}},
			0,
		}},
	}}}
	stage["device-meta"] = bson.M{"$mergeObjects": bson.A{
		bson.M{"$literal": StripMqttDeviceMeta(deviceMeta)},
		kept,
	}}

	return mongo.Pipeline{{{Key: "$set", Value: stage}}}
}

// DeviceCommand is a command document as stored in CommandsCollection.
//
// Output is kept as the raw BSON value: it is whatever JSON the device
// reported, stored BSON-quoted like device-meta, and View turns it back into
// plain JSON.
type DeviceCommand struct {
	ID         primitive.ObjectID     `bson:"_id"`
	DeviceID   primitive.ObjectID     `bson:"device_id"`
	Owner      string                 `bson:"owner"`
	CreatedBy  string                 `bson:"created_by"`
	Cmd        string                 `bson:"cmd"`
	Args       map[string]interface{} `bson:"args"`
	Status     string                 `bson:"status"`
	Code       int                    `bson:"code"`
	Output     bson.RawValue          `bson:"output,omitempty"`
	Error      string                 `bson:"error"`
	CreatedAt  time.Time              `bson:"created_at"`
	ExpiresAt  time.Time              `bson:"expires_at"`
	FinishedAt *time.Time             `bson:"finished_at,omitempty"`
}

// DeviceCommandSummary is a command as the history lists it: everything but
// the output, which can be up to 256 KiB per command. The output is only
// returned for one command at a time (DeviceCommandView).
type DeviceCommandSummary struct {
	ID         string                 `json:"id"`
	DeviceID   string                 `json:"device_id"`
	Cmd        string                 `json:"cmd"`
	Args       map[string]interface{} `json:"args"`
	Status     string                 `json:"status"`
	Code       int                    `json:"code"`
	Error      string                 `json:"error"`
	CreatedAt  time.Time              `json:"created_at"`
	ExpiresAt  time.Time              `json:"expires_at"`
	FinishedAt *time.Time             `json:"finished_at"`
	CreatedBy  string                 `json:"created_by"`
}

// DeviceCommandView is a command as the API returns it, output included.
type DeviceCommandView struct {
	DeviceCommandSummary
	Output interface{} `json:"output"`
}

// DeviceCommandRequest is the body of POST /devices/{id}/commands.
type DeviceCommandRequest struct {
	Cmd  string                 `json:"cmd"`
	Args map[string]interface{} `json:"args"`
}

// View renders a stored command for the API at time now. A command still
// pending after CommandTimeout is reported as timed out; the stored status is
// left alone, so a late result is still recorded.
func (cmd *DeviceCommand) View(now time.Time) DeviceCommandView {
	return DeviceCommandView{
		DeviceCommandSummary: cmd.Summary(now),
		Output:               DecodeCommandOutput(cmd.Output),
	}
}

// Summary renders a stored command for the history, without its output.
func (cmd *DeviceCommand) Summary(now time.Time) DeviceCommandSummary {
	args := cmd.Args
	if args == nil {
		args = map[string]interface{}{}
	}

	return DeviceCommandSummary{
		ID:         cmd.ID.Hex(),
		DeviceID:   cmd.DeviceID.Hex(),
		Cmd:        cmd.Cmd,
		Args:       args,
		Status:     EffectiveCommandStatus(cmd.Status, cmd.CreatedAt, now),
		Code:       cmd.Code,
		Error:      cmd.Error,
		CreatedAt:  cmd.CreatedAt,
		ExpiresAt:  cmd.ExpiresAt,
		FinishedAt: cmd.FinishedAt,
		CreatedBy:  cmd.CreatedBy,
	}
}

// EffectiveCommandStatus is the status the API reports: the stored one, except
// that a command pending for longer than CommandTimeout is a timeout.
func EffectiveCommandStatus(status string, createdAt, now time.Time) string {
	if status == CommandStatusPending && now.Sub(createdAt) > CommandTimeout {
		return CommandStatusTimeout
	}
	return status
}

// ValidateCommandRequest checks a command against the allowlist and returns
// the arguments to store: the optional string message for commands that take
// one, nothing for the rest. Unknown arguments are dropped, not refused.
func ValidateCommandRequest(req DeviceCommandRequest) (map[string]interface{}, error) {
	takesMessage, ok := DeviceCommands[req.Cmd]
	if !ok {
		return nil, errors.New("unknown command: " + req.Cmd)
	}

	args := map[string]interface{}{}
	if !takesMessage {
		return args, nil
	}

	raw, present := req.Args[CommandArgMessage]
	if !present || raw == nil {
		return args, nil
	}
	message, isString := raw.(string)
	if !isString {
		return nil, errors.New("args.message must be a string")
	}
	if len(message) > maxCommandMessageLength {
		return nil, errors.New("args.message is longer than " + strconv.Itoa(maxCommandMessageLength) + " bytes")
	}
	args[CommandArgMessage] = message

	return args, nil
}

// Output quoting sentinels, the ones BsonQuoteMap uses: stored output reads
// exactly like device-meta.
const (
	quotedDot    = "\uFF2E"
	quotedDollar = "\uFFE0"
)

// EncodeCommandOutput prepares a device-reported output value for storage.
// Output is arbitrary JSON, so it is BSON-quoted exactly like device-meta:
// dots in keys and every '$' are replaced by sentinels, which keeps keys legal
// field names and operators out of stored documents. nil stays nil.
//
// Unlike BsonQuoteMap it does not round-trip through encoding/json, so a
// json.Number (decode with UseNumber) is stored as an integer when it is one:
// ids above 2^53 keep every digit.
func EncodeCommandOutput(output interface{}) interface{} {
	switch v := output.(type) {
	case map[string]interface{}:
		quoted := make(map[string]interface{}, len(v))
		for key, value := range v {
			quoted[strings.ReplaceAll(utils.BsonQuote(key), "$", quotedDollar)] = EncodeCommandOutput(value)
		}
		return quoted
	case []interface{}:
		quoted := make([]interface{}, len(v))
		for i, value := range v {
			quoted[i] = EncodeCommandOutput(value)
		}
		return quoted
	case string:
		return strings.ReplaceAll(v, "$", quotedDollar)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		f, _ := v.Float64()
		return f
	default:
		return v
	}
}

// DecodeCommandOutput is the inverse of EncodeCommandOutput for a stored
// value, returning something encoding/json renders as the JSON the device
// sent. A missing or null output is nil.
func DecodeCommandOutput(raw bson.RawValue) interface{} {
	if raw.Type == 0 || raw.Type == bson.TypeNull || raw.Type == bson.TypeUndefined {
		return nil
	}

	var value interface{}
	if err := raw.Unmarshal(&value); err != nil {
		return nil
	}
	return unquoteCommandOutput(value)
}

func unquoteCommandOutput(value interface{}) interface{} {
	unquoteKey := func(key string) string {
		return strings.ReplaceAll(strings.ReplaceAll(key, quotedDot, "."), quotedDollar, "$")
	}

	switch v := value.(type) {
	case primitive.D:
		out := make(map[string]interface{}, len(v))
		for _, e := range v {
			out[unquoteKey(e.Key)] = unquoteCommandOutput(e.Value)
		}
		return out
	case primitive.M:
		out := make(map[string]interface{}, len(v))
		for key, e := range v {
			out[unquoteKey(key)] = unquoteCommandOutput(e)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, e := range v {
			out[unquoteKey(key)] = unquoteCommandOutput(e)
		}
		return out
	case primitive.A:
		out := make([]interface{}, len(v))
		for i, e := range v {
			out[i] = unquoteCommandOutput(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, e := range v {
			out[i] = unquoteCommandOutput(e)
		}
		return out
	case string:
		return strings.ReplaceAll(v, quotedDollar, "$")
	default:
		return v
	}
}

// PostCommandScopes may send commands. Commands act on the device itself
// (reboot, SSH), so the general devices / devices.write scopes an OAuth app
// may hold do not grant them: it takes the full API scope or the dedicated
// devices.commands one.
var PostCommandScopes = []utils.Scope{
	utils.Scopes.API,
	utils.Scopes.DeviceCommands,
}

// readCommandScopes may read commands: the device read scopes, and
// devices.commands, so a client allowed to send a command can also read its
// result.
func readCommandScopes(readDevicesScopes []utils.Scope) []utils.Scope {
	return append([]utils.Scope{utils.Scopes.DeviceCommands}, readDevicesScopes...)
}

// Per-device rate limits on sending commands, over a sliding minute. Kept in
// Mongo so they hold across API replicas.
const (
	commandRateWindow = time.Minute
	// commandRateLimit caps all commands to one device.
	commandRateLimit = 10
	// A device is rebooted at most once per commandRateWindow: a loop of
	// reboots would keep the device down for good.
	rebootCommand = "REBOOT_DEVICE"
)

// CommandLimitsCollection holds the rate-limit state of each device that was
// sent a command: {_id: <device id>, sent: [<times of the last
// commandRateLimit commands>], reboot_at: <time of the last reboot>,
// updated_at}. Documents idle for commandLimitsIdle are dropped by a TTL
// index; by then they limit nothing.
const CommandLimitsCollection = "pantahub_device_command_limits"

const commandLimitsIdle = time.Hour

// errCommandRateLimited is answered with 429.
var errCommandRateLimited = errors.New("too many commands for this device, try again in a minute")

// reserveCommandSlot takes one of the device's command slots, or refuses with
// errCommandRateLimited. Checking and taking the slot is a single conditional
// upsert, so concurrent requests, on any replica, cannot both take the last
// slot: the one that finds the limit reached matches nothing, and its upsert
// then collides with the device's existing document (a duplicate key).
func (a *App) reserveCommandSlot(ctx context.Context, deviceID primitive.ObjectID, cmd string, now time.Time) error {
	windowStart := now.Add(-commandRateWindow)

	conditions := bson.A{
		// Fewer than commandRateLimit commands ever, or the oldest of the
		// last commandRateLimit is out of the window.
		bson.M{"$or": bson.A{
			bson.M{"sent." + strconv.Itoa(commandRateLimit-1): bson.M{"$exists": false}},
			bson.M{"sent.0": bson.M{"$lt": windowStart}},
		}},
	}
	set := bson.M{"updated_at": now}
	if cmd == rebootCommand {
		conditions = append(conditions, bson.M{"$or": bson.A{
			bson.M{"reboot_at": bson.M{"$exists": false}},
			bson.M{"reboot_at": bson.M{"$lt": windowStart}},
		}})
		set["reboot_at"] = now
	}

	filter := bson.M{"_id": deviceID, "$and": conditions}
	update := bson.M{
		"$set": set,
		"$push": bson.M{"sent": bson.M{
			"$each":  bson.A{now},
			"$sort":  1,
			"$slice": -commandRateLimit,
		}},
	}

	limits := a.mongoClient.Database(utils.MongoDb).Collection(CommandLimitsCollection)
	// A duplicate key on the first attempt may also be two first commands to
	// the same device racing to create its document; the second attempt
	// finds the document and answers for real.
	for attempt := 0; attempt < 2; attempt++ {
		_, err := limits.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
		if err == nil {
			return nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			return err
		}
	}
	return errCommandRateLimited
}

// userError answers with a message meant for the caller: why a command was
// refused is part of the answer, unlike an internal error, which only gets
// an incident id.
func userError(c *echo.Context, message string, code int) error {
	return echoutil.RestErrorWrapperUser(c, message, message, code)
}

// callerPrn returns the PRN of the authenticated caller.
func callerPrn(c *echo.Context) (string, bool) {
	claims, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)
	if !ok {
		return "", false
	}
	prn, ok := claims["prn"].(string)
	return prn, ok && prn != ""
}

// findOwnedDevice loads a device of owner by id, PRN or nick. It reports
// (nil, nil) when there is no such device or it belongs to someone else, which
// the command endpoints answer the same way: 404.
func (a *App) findOwnedDevice(ctx context.Context, owner, ref string) (*Device, error) {
	deviceID, err := a.ResolveDeviceIDOrNick(ctx, owner, ref)
	if err != nil {
		if mongoutils.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	device := Device{}
	err = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices").FindOne(ctxC, bson.M{
		"_id":     deviceID,
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}, options.FindOne().SetProjection(bson.M{"_id": 1, "prn": 1, "owner": 1, "device-meta": 1})).Decode(&device)
	if mongoutils.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &device, nil
}

// mqttBroker returns the broker replica the device is recorded as connected
// to, if any. Device-meta keys are stored BSON-quoted, so the dotted keys are
// looked up in their quoted form.
func mqttBroker(deviceMeta map[string]interface{}) (string, bool) {
	connected, _ := deviceMeta[utils.BsonQuote(DeviceMetaMqttConnected)].(bool)
	broker, _ := deviceMeta[utils.BsonQuote(DeviceMetaMqttBroker)].(string)
	return broker, connected && broker != ""
}

// mqttConnected reports whether the device holds an MQTT connection commands
// can be delivered on: the broker recorded it as connected, and the replica
// holding the connection is still alive.
func (a *App) mqttConnected(ctx context.Context, deviceMeta map[string]interface{}) (bool, error) {
	broker, ok := mqttBroker(deviceMeta)
	if !ok {
		return false, nil
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := a.mongoClient.Database(utils.MongoDb).Collection(MqttBrokersCollection).FindOne(ctxC, bson.M{
		"_id":          broker,
		"heartbeat_at": bson.M{"$gte": time.Now().Add(-MqttBrokerLease)},
	}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// handlePostCommand sends a command to a device connected over MQTT
// @Summary Send a command to a device connected over MQTT
// @Description Queues one of the allowlisted commands (REBOOT_DEVICE, RUN_GC,
// @Description ENABLE_SSH, DISABLE_SSH, LIST_CONTAINERS, LIST_GROUPS, LIST_DAEMONS,
// @Description LIST_DRIVERS, LIST_WAKELOCKS, GET_XCONNECT_GRAPH) for delivery over MQTT.
// @Description REBOOT_DEVICE accepts an optional string args.message; other arguments are ignored.
// @Description Only the device owner may send commands, and only while the device is connected over MQTT.
// @Description Requires the full API scope or devices.commands. At most 10 commands per device per minute, and 1 REBOOT_DEVICE.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body DeviceCommandRequest true "Command"
// @Success 201 {object} DeviceCommandView
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 409 {object} utils.RError
// @Failure 413 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/commands [post]
func (a *App) handlePostCommand(c *echo.Context) error {
	caller, ok := callerPrn(c)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return userError(c, "Device not found", http.StatusNotFound)
	}

	req := DeviceCommandRequest{}
	c.Request().Body = http.MaxBytesReader(nil, c.Request().Body, maxCommandRequestSize)
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return userError(c, "Command request is larger than "+strconv.Itoa(maxCommandRequestSize)+" bytes", http.StatusRequestEntityTooLarge)
		}
		return userError(c, "Error parsing command: "+err.Error(), http.StatusBadRequest)
	}
	args, err := ValidateCommandRequest(req)
	if err != nil {
		return userError(c, err.Error(), http.StatusBadRequest)
	}

	connected, err := a.mqttConnected(c.Request().Context(), device.DeviceMeta)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking the MQTT connection: "+err.Error(), http.StatusInternalServerError)
	}
	if !connected {
		return userError(c, "Device is not connected over MQTT", http.StatusConflict)
	}

	// Whole seconds: expires_at goes on the wire as RFC 3339 without a
	// fraction, and the stored times must read back as they were returned.
	now := time.Now().UTC().Truncate(time.Second)
	command := DeviceCommand{
		ID:        primitive.NewObjectID(),
		DeviceID:  device.ID,
		Owner:     device.Owner,
		CreatedBy: caller,
		Cmd:       req.Cmd,
		Args:      args,
		Status:    CommandStatusPending,
		CreatedAt: now,
		ExpiresAt: now.Add(CommandExpiry),
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	if err := a.reserveCommandSlot(ctx, device.ID, req.Cmd, time.Now()); err != nil {
		if errors.Is(err, errCommandRateLimited) {
			return userError(c, err.Error(), http.StatusTooManyRequests)
		}
		return echoutil.RestErrorWrapper(c, "Error checking command rate: "+err.Error(), http.StatusInternalServerError)
	}

	// The insert is the send: the MQTT notifier on every replica watches this
	// collection and publishes the command to the device.
	_, err = a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection).InsertOne(ctx, command)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error storing command: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusCreated, command.View(time.Now()))
}

// handleGetCommands lists the most recent commands sent to a device
// @Summary List the most recent commands sent to a device
// @Description Newest first. A command still pending after 120 seconds is reported with status "timeout".
// @Description The output of a command is not listed: GET /devices/{id}/commands/{cid} returns it.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param limit query int false "Maximum number of commands (default 20, max 100)"
// @Success 200 {array} DeviceCommandSummary
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/commands [get]
func (a *App) handleGetCommands(c *echo.Context) error {
	caller, ok := callerPrn(c)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	limit, err := parseCommandsLimit(c.QueryParam("limit"))
	if err != nil {
		return userError(c, err.Error(), http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return userError(c, "Device not found", http.StatusNotFound)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	filter, opts := commandHistoryQuery(device.ID, caller, limit)
	cursor, err := a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection).Find(ctx, filter, opts)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error listing commands: "+err.Error(), http.StatusInternalServerError)
	}
	defer cursor.Close(ctx)

	now := time.Now()
	summaries := make([]DeviceCommandSummary, 0, limit)
	for cursor.Next(ctx) {
		command := DeviceCommand{}
		if err := cursor.Decode(&command); err != nil {
			log.Println("devices: cannot decode command: " + err.Error())
			continue
		}
		summaries = append(summaries, command.Summary(now))
	}
	if err := cursor.Err(); err != nil {
		return echoutil.RestErrorWrapper(c, "Error listing commands: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, summaries)
}

// handleGetCommand gets one command sent to a device
// @Summary Get one command sent to a device
// @Description A command still pending after 120 seconds is reported with status "timeout".
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param cid path string true "Command ID"
// @Success 200 {object} DeviceCommandView
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/commands/{cid} [get]
func (a *App) handleGetCommand(c *echo.Context) error {
	caller, ok := callerPrn(c)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	commandID, err := primitive.ObjectIDFromHex(c.Param("cid"))
	if err != nil {
		return userError(c, "Invalid command id", http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return userError(c, "Device not found", http.StatusNotFound)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	command := DeviceCommand{}
	err = a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection).FindOne(ctx, bson.M{
		"_id":       commandID,
		"device_id": device.ID,
		"owner":     caller,
	}).Decode(&command)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return userError(c, "Command not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading command: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, command.View(time.Now()))
}

// commandHistoryQuery is the query behind the command history: the newest
// commands of one device sent by its owner, without their output. It is
// answered through commandHistoryIndex, reading only the documents it returns.
func commandHistoryQuery(deviceID primitive.ObjectID, owner string, limit int) (bson.M, *options.FindOptions) {
	return bson.M{"device_id": deviceID, "owner": owner},
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit)).
			SetProjection(bson.M{"output": 0})
}

// commandHistoryIndex matches commandHistoryQuery: equality on the device and
// its owner, then the sort.
var commandHistoryIndex = bson.D{
	{Key: "device_id", Value: int32(1)},
	{Key: "owner", Value: int32(1)},
	{Key: "created_at", Value: int32(-1)},
	{Key: "_id", Value: int32(-1)},
}

// supersededCommandIndices were created by earlier versions and are covered
// by commandHistoryIndex.
var supersededCommandIndices = []string{"device_id_1_created_at_-1"}

// DeleteDeviceCommands removes the commands of a device and its rate-limit
// state, for a device that is being deleted.
func (a *App) DeleteDeviceCommands(ctx context.Context, deviceID primitive.ObjectID) error {
	db := a.mongoClient.Database(utils.MongoDb)
	if _, err := db.Collection(CommandsCollection).DeleteMany(ctx, bson.M{"device_id": deviceID}); err != nil {
		return err
	}
	_, err := db.Collection(CommandLimitsCollection).DeleteOne(ctx, bson.M{"_id": deviceID})
	return err
}

// parseCommandsLimit reads ?limit: absent means the default, anything above
// the maximum is clamped to it, and a non-positive or non-numeric value is an
// error.
func parseCommandsLimit(raw string) (int, error) {
	if raw == "" {
		return commandsDefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 0, errors.New("limit must be a positive integer")
	}
	if limit > commandsMaxLimit {
		limit = commandsMaxLimit
	}
	return limit, nil
}

// EnsureCommandIndices creates the index the command history reads use, the
// TTL index that enforces CommandRetention and the TTL index that drops idle
// rate-limit state.
func (a *App) EnsureCommandIndices() error {
	ctx, cancel := context.WithTimeout(context.Background(), CreateIndexTimeout)
	defer cancel()

	collection := a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection)
	_, err := collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: commandHistoryIndex},
		{
			Keys:    bson.D{{Key: "created_at", Value: int32(1)}},
			Options: options.Index().SetExpireAfterSeconds(int32(CommandRetention.Seconds())),
		},
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	if err != nil {
		return err
	}
	for _, name := range supersededCommandIndices {
		// Absent is fine: a new deployment never had it.
		if _, err := collection.Indexes().DropOne(ctx, name); err != nil && !isIndexNotFound(err) {
			return err
		}
	}

	limits := a.mongoClient.Database(utils.MongoDb).Collection(CommandLimitsCollection)
	_, err = limits.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "updated_at", Value: int32(1)}},
		Options: options.Index().SetExpireAfterSeconds(int32(commandLimitsIdle.Seconds())),
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))

	return err
}

// isIndexNotFound reports a dropIndexes error for an index that does not
// exist (or a collection that does not exist yet).
func isIndexNotFound(err error) bool {
	var serverErr mongo.ServerError
	return errors.As(err, &serverErr) && (serverErr.HasErrorCode(27) || serverErr.HasErrorCode(26))
}
