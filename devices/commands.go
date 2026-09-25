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
	"errors"
	"log"
	"net/http"
	"strconv"
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

	commandsDefaultLimit = 20
	commandsMaxLimit     = 100
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

// DeviceMetaMqttConnected is the device-meta key the MQTT bridge keeps true
// while the device is connected over MQTT. Commands are only accepted then.
const DeviceMetaMqttConnected = "pantahub.mqtt.connected"

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

// DeviceCommandView is a command as the API returns it.
type DeviceCommandView struct {
	ID         string                 `json:"id"`
	DeviceID   string                 `json:"device_id"`
	Cmd        string                 `json:"cmd"`
	Args       map[string]interface{} `json:"args"`
	Status     string                 `json:"status"`
	Code       int                    `json:"code"`
	Output     interface{}            `json:"output"`
	Error      string                 `json:"error"`
	CreatedAt  time.Time              `json:"created_at"`
	ExpiresAt  time.Time              `json:"expires_at"`
	FinishedAt *time.Time             `json:"finished_at"`
	CreatedBy  string                 `json:"created_by"`
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
	args := cmd.Args
	if args == nil {
		args = map[string]interface{}{}
	}

	return DeviceCommandView{
		ID:         cmd.ID.Hex(),
		DeviceID:   cmd.DeviceID.Hex(),
		Cmd:        cmd.Cmd,
		Args:       args,
		Status:     EffectiveCommandStatus(cmd.Status, cmd.CreatedAt, now),
		Code:       cmd.Code,
		Output:     DecodeCommandOutput(cmd.Output),
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

// EncodeCommandOutput prepares a device-reported output value for storage.
// Output is arbitrary JSON, so it is BSON-quoted exactly like device-meta:
// dots in keys and every '$' are replaced by sentinels, which keeps keys legal
// field names and operators out of stored documents. nil stays nil.
func EncodeCommandOutput(output interface{}) interface{} {
	if output == nil {
		return nil
	}
	wrapped := map[string]interface{}{"v": output}
	return utils.BsonQuoteMap(&wrapped)["v"]
}

// DecodeCommandOutput is the inverse of EncodeCommandOutput for a stored
// value, returning something encoding/json renders as the JSON the device
// sent. A missing or null output is nil.
func DecodeCommandOutput(raw bson.RawValue) interface{} {
	if raw.Type == 0 || raw.Type == bson.TypeNull || raw.Type == bson.TypeUndefined {
		return nil
	}

	// Decoding through a document lets nested documents come back as maps
	// rather than ordered key/value slices, which encode to JSON as objects.
	doc, err := bson.Marshal(bson.D{{Key: "v", Value: raw}})
	if err != nil {
		return nil
	}
	wrapped := map[string]interface{}{}
	if err := bson.Unmarshal(doc, &wrapped); err != nil {
		return nil
	}

	return utils.BsonUnquoteMap(&wrapped)["v"]
}

// PostCommandScopes may send commands. Commands act on the device itself
// (reboot, SSH), so the general devices / devices.write scopes an OAuth app
// may hold do not grant them: it takes the full API scope or the dedicated
// devices.commands one. Reading command history keeps the device read scopes.
var PostCommandScopes = []utils.Scope{
	utils.Scopes.API,
	utils.Scopes.DeviceCommands,
}

// Per-device rate limits on sending commands, counted over the last minute.
// Counted in Mongo so they hold across API replicas.
const (
	commandRateWindow = time.Minute
	// commandRateLimit caps all commands to one device.
	commandRateLimit = 10
	// rebootRateLimit caps reboots: a loop of them would keep the device
	// down for good.
	rebootRateLimit = 1
)

// errCommandRateLimited is answered with 429.
var errCommandRateLimited = errors.New("too many commands for this device, try again in a minute")

// checkCommandRate refuses a command that would exceed the per-device limits.
func (a *App) checkCommandRate(ctx context.Context, deviceID primitive.ObjectID, cmd string, now time.Time) error {
	commands := a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection)
	since := bson.M{"$gte": now.Add(-commandRateWindow)}

	total, err := commands.CountDocuments(ctx, bson.M{"device_id": deviceID, "created_at": since})
	if err != nil {
		return err
	}
	if total >= commandRateLimit {
		return errCommandRateLimited
	}
	if cmd == "REBOOT_DEVICE" {
		reboots, err := commands.CountDocuments(ctx, bson.M{"device_id": deviceID, "cmd": cmd, "created_at": since})
		if err != nil {
			return err
		}
		if reboots >= rebootRateLimit {
			return errCommandRateLimited
		}
	}
	return nil
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

// mqttConnected reports whether the MQTT bridge last recorded the device as
// connected. Device-meta keys are stored BSON-quoted, so the dotted key is
// looked up in its quoted form.
func mqttConnected(deviceMeta map[string]interface{}) bool {
	connected, _ := deviceMeta[utils.BsonQuote(DeviceMetaMqttConnected)].(bool)
	return connected
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
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}

	req := DeviceCommandRequest{}
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		return echoutil.RestErrorWrapper(c, "Error parsing command: "+err.Error(), http.StatusBadRequest)
	}
	args, err := ValidateCommandRequest(req)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
	}

	if !mqttConnected(device.DeviceMeta) {
		return echoutil.RestErrorWrapper(c, "Device is not connected over MQTT", http.StatusConflict)
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

	if err := a.checkCommandRate(ctx, device.ID, req.Cmd, now); err != nil {
		if errors.Is(err, errCommandRateLimited) {
			return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusTooManyRequests)
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
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param limit query int false "Maximum number of commands (default 20, max 100)"
// @Success 200 {array} DeviceCommandView
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
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	cursor, err := a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection).Find(ctx,
		bson.M{"device_id": device.ID, "owner": caller},
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error listing commands: "+err.Error(), http.StatusInternalServerError)
	}
	defer cursor.Close(ctx)

	now := time.Now()
	views := make([]DeviceCommandView, 0, limit)
	for cursor.Next(ctx) {
		command := DeviceCommand{}
		if err := cursor.Decode(&command); err != nil {
			log.Println("devices: cannot decode command: " + err.Error())
			continue
		}
		views = append(views, command.View(now))
	}
	if err := cursor.Err(); err != nil {
		return echoutil.RestErrorWrapper(c, "Error listing commands: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, views)
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
		return echoutil.RestErrorWrapper(c, "Invalid command id", http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
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
		return echoutil.RestErrorWrapper(c, "Command not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading command: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, command.View(time.Now()))
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

// EnsureCommandIndices creates the index the command history reads use.
func (a *App) EnsureCommandIndices() error {
	ctx, cancel := context.WithTimeout(context.Background(), CreateIndexTimeout)
	defer cancel()

	collection := a.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection)
	_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "device_id", Value: int32(1)},
			{Key: "created_at", Value: int32(-1)},
		},
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))

	return err
}
