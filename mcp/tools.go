//
// Copyright 2026 Pantacor Ltd.
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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const serverInstructions = `Pantahub manages fleets of Linux devices running Pantavisor.

A device is identified by its id or by its nick; every tool takes either. Each
device has a trail of numbered revisions. A revision is a desired system state
the device is asked to run, and its status reports how far the device got: NEW
and QUEUED mean the device has not picked it up yet, DOWNLOADING, INPROGRESS and
TESTING mean it is being installed, DONE means it is running, and WONTGO and
ERROR mean it failed.

Devices carry two metadata maps. device-meta is reported by the device itself
and is read-only here. user-meta is configuration the owner sets for the device.

Start from list_devices, use get_device_status to see whether a device is up to
date, and get_device_logs to find out why a revision failed.

update_user_meta changes a device's configuration. Device join tokens (what new
devices enrol with) and the account's OAuth applications can be listed and
edited, but not created or deleted here, and their secrets are never available:
send the user to the Pantahub web app for that. Tools that change something ask
the user first; say exactly what will change before calling one.`

const (
	defaultPageSize = 25
	maxPageSize     = 100

	defaultLogPageSize = 50
	maxLogPageSize     = 100

	// maxLogMessage and maxStateBytes keep a single result far below the size
	// MCP clients accept for a tool result, whatever a device decided to log
	// or however large a state document is.
	maxLogMessage = 1000
	maxStateBytes = 60000
)

const (
	toolListDevices     = "list_devices"
	toolGetDevice       = "get_device"
	toolGetDeviceStatus = "get_device_status"
	toolListRevisions   = "list_revisions"
	toolGetRevision     = "get_revision"
	toolGetDeviceLogs   = "get_device_logs"
)

// toolScopes names the scopes that unlock each tool (any one of them does). It
// is consulted twice: by the HTTP middleware, to answer with a proper 403
// challenge, and by the tool itself, which is what actually guards the data.
var toolScopes = map[string][]string{
	toolListDevices:     readDeviceScopes,
	toolGetDevice:       readDeviceScopes,
	toolGetDeviceStatus: readTrailScopes,
	toolListRevisions:   readTrailScopes,
	toolGetRevision:     readTrailScopes,
	toolGetDeviceLogs:   readDeviceScopes,
}

func readOnly(title string) *sdk.ToolAnnotations {
	closedWorld := false
	return &sdk.ToolAnnotations{
		Title:          title,
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  &closedWorld,
	}
}

func (s *Service) registerTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{
		Name:  toolListDevices,
		Title: "List devices",
		Description: "List the devices the user owns, without their metadata. " +
			"Results are paged: pass next_cursor back as cursor to get the next page.",
		Annotations: readOnly("List devices"),
	}, s.listDevices)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetDevice,
		Title: "Get device",
		Description: "Get one device with its user-meta (configuration set by the owner) " +
			"and device-meta (facts reported by the device, read-only).",
		Annotations: readOnly("Get device"),
	}, s.getDevice)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetDeviceStatus,
		Title: "Get device status",
		Description: "Tell whether a device is up to date: its newest revision with its " +
			"installation status, the last revision it reported as running, and when it was last seen.",
		Annotations: readOnly("Get device status"),
	}, s.getDeviceStatus)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolListRevisions,
		Title: "List revisions",
		Description: "List the revisions of a device, newest first, with commit message and " +
			"installation status. Pass next_before_revision back as before_revision for older ones.",
		Annotations: readOnly("List revisions"),
	}, s.listRevisions)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetRevision,
		Title: "Get revision",
		Description: "Get one revision of a device: status, progress, and the list of files in " +
			"its state. Set include_state to also get the state document itself.",
		Annotations: readOnly("Get revision"),
	}, s.getRevision)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetDeviceLogs,
		Title: "Get device logs",
		Description: "Read the logs a device uploaded, newest first. Filter by revision, level, " +
			"source or time window to find why an update failed.",
		Annotations: readOnly("Get device logs"),
	}, s.getDeviceLogs)

	s.registerManagementTools(server)
}

// authorize resolves the caller of a tool and checks its scopes. This is the
// check that guards the data; requireToolScopes only improves the error.
func authorize(req *sdk.CallToolRequest, tool string) (*identity, error) {
	if req == nil || req.Extra == nil {
		return nil, errors.New("not authenticated")
	}
	caller, err := identityFrom(req.Extra.TokenInfo)
	if err != nil {
		return nil, err
	}
	if !utils.MatchScope(toolScopes[tool], caller.Scopes) {
		return nil, errors.New("the access granted to this connection does not cover " + tool)
	}

	log.Printf("INFO: mcp tool=%s user=%s", tool, caller.Prn)
	return caller, nil
}

// toolError turns a storage error into what the assistant should read. Internal
// errors are logged, not shown: they may name hosts, collections or queries.
func toolError(tool string, err error) error {
	if errors.Is(err, errNotFound) {
		return errors.New("no such device or revision among the devices you own")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("the query took too long; narrow it down and try again")
	}
	log.Printf("ERROR: mcp tool=%s: %s", tool, err.Error())
	return errors.New("internal error while running " + tool)
}

func pageSize(requested, fallback, max int) int64 {
	if requested <= 0 {
		return int64(fallback)
	}
	if requested > max {
		return int64(max)
	}
	return int64(requested)
}

func formatTime(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// deviceSummary is a device without its metadata maps.
type deviceSummary struct {
	ID           string `json:"id"`
	Nick         string `json:"nick"`
	Prn          string `json:"prn"`
	Public       bool   `json:"public" jsonschema:"whether anyone can see this device"`
	TimeCreated  string `json:"time_created,omitempty"`
	TimeModified string `json:"time_modified,omitempty"`
	LastSeen     string `json:"last_seen,omitempty" jsonschema:"last time the device reported its device-meta"`
}

func summarize(device *devices.Device) deviceSummary {
	return deviceSummary{
		ID:           device.ID.Hex(),
		Nick:         device.Nick,
		Prn:          device.Prn,
		Public:       device.IsPublic,
		TimeCreated:  formatTime(device.TimeCreated),
		TimeModified: formatTime(device.TimeModified),
		LastSeen:     formatTime(device.MetaModified),
	}
}

// revisionSummary is a revision without its state.
type revisionSummary struct {
	Revision     int    `json:"revision"`
	CommitMsg    string `json:"commit_msg,omitempty"`
	Committer    string `json:"committer,omitempty"`
	Status       string `json:"status" jsonschema:"NEW, QUEUED, DOWNLOADING, INPROGRESS, TESTING, UPDATED, DONE, WONTGO or ERROR"`
	StatusMsg    string `json:"status_msg,omitempty"`
	Progress     int    `json:"progress" jsonschema:"installation progress, 0 to 100"`
	Retries      int    `json:"retries,omitempty"`
	StepTime     string `json:"step_time,omitempty" jsonschema:"when the revision was posted"`
	ProgressTime string `json:"progress_time,omitempty" jsonschema:"when the device last reported progress on it"`
}

func summarizeStep(step *trailmodels.Step) revisionSummary {
	return revisionSummary{
		Revision:     step.Rev,
		CommitMsg:    step.CommitMsg,
		Committer:    step.Committer,
		Status:       step.StepProgress.Status,
		StatusMsg:    step.StepProgress.StatusMsg,
		Progress:     step.StepProgress.Progress,
		Retries:      step.StepProgress.Retries,
		StepTime:     formatTime(step.StepTime),
		ProgressTime: formatTime(step.ProgressTime),
	}
}

type deviceRef struct {
	Device string `json:"device" jsonschema:"device id or nick"`
}

// list_devices

type listDevicesInput struct {
	NickPrefix string `json:"nick_prefix,omitempty" jsonschema:"only devices whose nick starts with this"`
	Limit      int    `json:"limit,omitempty" jsonschema:"page size, 25 by default and 100 at most"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
}

type listDevicesOutput struct {
	Devices    []deviceSummary `json:"devices"`
	NextCursor string          `json:"next_cursor,omitempty" jsonschema:"present when more devices follow"`
}

func (s *Service) listDevices(ctx context.Context, req *sdk.CallToolRequest, in listDevicesInput) (*sdk.CallToolResult, listDevicesOutput, error) {
	out := listDevicesOutput{Devices: []deviceSummary{}}

	caller, err := authorize(req, toolListDevices)
	if err != nil {
		return nil, out, err
	}

	page, more, err := s.store.listDevices(ctx, caller.Prn, in.NickPrefix, in.Cursor,
		pageSize(in.Limit, defaultPageSize, maxPageSize))
	if err != nil {
		if err.Error() == "invalid cursor" {
			return nil, out, errors.New("cursor is not a next_cursor returned by list_devices")
		}
		return nil, out, toolError(toolListDevices, err)
	}

	for i := range page {
		out.Devices = append(out.Devices, summarize(&page[i]))
	}
	if more {
		out.NextCursor = page[len(page)-1].ID.Hex()
	}
	return nil, out, nil
}

// get_device

type getDeviceOutput struct {
	deviceSummary
	OwnershipUnverified bool                   `json:"ownership_unverified,omitempty"`
	UserMeta            map[string]interface{} `json:"user_meta" jsonschema:"configuration set by the owner"`
	DeviceMeta          map[string]interface{} `json:"device_meta" jsonschema:"facts reported by the device, read-only"`
}

func (s *Service) getDevice(ctx context.Context, req *sdk.CallToolRequest, in deviceRef) (*sdk.CallToolResult, getDeviceOutput, error) {
	out := getDeviceOutput{}

	caller, err := authorize(req, toolGetDevice)
	if err != nil {
		return nil, out, err
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolGetDevice, err)
	}

	out.deviceSummary = summarize(device)
	out.OwnershipUnverified = device.OwnershipUnverify
	out.UserMeta = nonNil(device.UserMeta)
	out.DeviceMeta = nonNil(device.DeviceMeta)
	return nil, out, nil
}

func nonNil(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

// get_device_status

type getDeviceStatusOutput struct {
	Device         deviceSummary    `json:"device"`
	LatestRevision *revisionSummary `json:"latest_revision,omitempty" jsonschema:"the newest revision posted for the device"`
	LastDone       *revisionSummary `json:"last_done_revision,omitempty" jsonschema:"the newest revision the device reported as DONE, which is normally the one it runs"`
	UpToDate       bool             `json:"up_to_date" jsonschema:"true when the newest revision is DONE"`
}

func (s *Service) getDeviceStatus(ctx context.Context, req *sdk.CallToolRequest, in deviceRef) (*sdk.CallToolResult, getDeviceStatusOutput, error) {
	out := getDeviceStatusOutput{}

	caller, err := authorize(req, toolGetDeviceStatus)
	if err != nil {
		return nil, out, err
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolGetDeviceStatus, err)
	}
	out.Device = summarize(device)

	latest, err := s.store.getStep(ctx, caller.Prn, device.ID, -1, "")
	if errors.Is(err, errNotFound) {
		// A claimed device that never had a trail created: nothing to report.
		return nil, out, nil
	}
	if err != nil {
		return nil, out, toolError(toolGetDeviceStatus, err)
	}
	latestSummary := summarizeStep(latest)
	out.LatestRevision = &latestSummary

	if latest.StepProgress.Status == statusDone {
		out.LastDone = &latestSummary
		out.UpToDate = true
		return nil, out, nil
	}

	done, err := s.store.getStep(ctx, caller.Prn, device.ID, -1, statusDone)
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, out, toolError(toolGetDeviceStatus, err)
	}
	if err == nil {
		doneSummary := summarizeStep(done)
		out.LastDone = &doneSummary
	}
	return nil, out, nil
}

// list_revisions

type listRevisionsInput struct {
	Device         string `json:"device" jsonschema:"device id or nick"`
	Limit          int    `json:"limit,omitempty" jsonschema:"page size, 25 by default and 100 at most"`
	BeforeRevision int    `json:"before_revision,omitempty" jsonschema:"only revisions older than this one"`
}

type listRevisionsOutput struct {
	Revisions          []revisionSummary `json:"revisions"`
	NextBeforeRevision int               `json:"next_before_revision,omitempty" jsonschema:"present when older revisions follow"`
}

func (s *Service) listRevisions(ctx context.Context, req *sdk.CallToolRequest, in listRevisionsInput) (*sdk.CallToolResult, listRevisionsOutput, error) {
	out := listRevisionsOutput{Revisions: []revisionSummary{}}

	caller, err := authorize(req, toolListRevisions)
	if err != nil {
		return nil, out, err
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolListRevisions, err)
	}

	page, more, err := s.store.listSteps(ctx, caller.Prn, device.ID, in.BeforeRevision,
		pageSize(in.Limit, defaultPageSize, maxPageSize))
	if err != nil {
		return nil, out, toolError(toolListRevisions, err)
	}

	for i := range page {
		out.Revisions = append(out.Revisions, summarizeStep(&page[i]))
	}
	if more {
		out.NextBeforeRevision = page[len(page)-1].Rev
	}
	return nil, out, nil
}

// get_revision

type getRevisionInput struct {
	Device       string `json:"device" jsonschema:"device id or nick"`
	Revision     *int   `json:"revision,omitempty" jsonschema:"revision number; the newest one when omitted"`
	IncludeState bool   `json:"include_state,omitempty" jsonschema:"also return the state document"`
}

type getRevisionOutput struct {
	revisionSummary
	StateSha     string                 `json:"state_sha,omitempty"`
	StateFiles   []string               `json:"state_files" jsonschema:"paths of the files that make up the state"`
	State        map[string]interface{} `json:"state,omitempty" jsonschema:"the state document, when asked for and not too large"`
	StateOmitted string                 `json:"state_omitted,omitempty" jsonschema:"why the state document was left out"`
	Meta         map[string]interface{} `json:"meta,omitempty"`
}

func (s *Service) getRevision(ctx context.Context, req *sdk.CallToolRequest, in getRevisionInput) (*sdk.CallToolResult, getRevisionOutput, error) {
	out := getRevisionOutput{StateFiles: []string{}}

	caller, err := authorize(req, toolGetRevision)
	if err != nil {
		return nil, out, err
	}

	rev := -1
	if in.Revision != nil {
		if *in.Revision < 0 {
			return nil, out, errors.New("revision cannot be negative")
		}
		rev = *in.Revision
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolGetRevision, err)
	}
	step, err := s.store.getStep(ctx, caller.Prn, device.ID, rev, "")
	if err != nil {
		return nil, out, toolError(toolGetRevision, err)
	}

	out.revisionSummary = summarizeStep(step)
	out.StateSha = step.StateSha
	out.Meta = step.Meta
	for path := range step.State {
		out.StateFiles = append(out.StateFiles, path)
	}
	sort.Strings(out.StateFiles)

	if in.IncludeState {
		encoded, err := json.Marshal(step.State)
		switch {
		case err != nil:
			out.StateOmitted = "the state document could not be encoded"
		case len(encoded) > maxStateBytes:
			out.StateOmitted = fmt.Sprintf("the state document is %d bytes, too large to return; see state_files", len(encoded))
		default:
			out.State = step.State
		}
	}
	return nil, out, nil
}

// get_device_logs

type getDeviceLogsInput struct {
	Device   string `json:"device" jsonschema:"device id or nick"`
	Revision string `json:"revision,omitempty" jsonschema:"only logs written while this revision was running"`
	Level    string `json:"level,omitempty" jsonschema:"only this log level, for example ERROR, WARN, INFO or DEBUG"`
	Source   string `json:"source,omitempty" jsonschema:"only this log source, for example a container or file name"`
	After    string `json:"after,omitempty" jsonschema:"only logs after this RFC 3339 time"`
	Before   string `json:"before,omitempty" jsonschema:"only logs before this RFC 3339 time"`
	Limit    int    `json:"limit,omitempty" jsonschema:"page size, 50 by default and 100 at most"`
	Offset   int    `json:"offset,omitempty" jsonschema:"next_offset of the previous page"`
}

type logLine struct {
	Time     string `json:"time,omitempty"`
	Level    string `json:"level,omitempty"`
	Source   string `json:"source,omitempty"`
	Revision string `json:"revision,omitempty"`
	Platform string `json:"platform,omitempty"`
	Message  string `json:"message"`
}

type getDeviceLogsOutput struct {
	Logs       []logLine `json:"logs"`
	NextOffset int       `json:"next_offset,omitempty" jsonschema:"present when older logs may follow"`
}

func (s *Service) getDeviceLogs(ctx context.Context, req *sdk.CallToolRequest, in getDeviceLogsInput) (*sdk.CallToolResult, getDeviceLogsOutput, error) {
	out := getDeviceLogsOutput{Logs: []logLine{}}

	caller, err := authorize(req, toolGetDeviceLogs)
	if err != nil {
		return nil, out, err
	}

	before, err := parseTime("before", in.Before)
	if err != nil {
		return nil, out, err
	}
	after, err := parseTime("after", in.After)
	if err != nil {
		return nil, out, err
	}
	if in.Offset < 0 {
		return nil, out, errors.New("offset cannot be negative")
	}

	// The device is resolved against the caller's own devices first, so the
	// log query can only ever name a device the caller owns.
	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolGetDeviceLogs, err)
	}

	limit := pageSize(in.Limit, defaultLogPageSize, maxLogPageSize)
	pager, err := s.store.getLogs(ctx, logs.Query{
		Owner:  caller.Prn,
		Device: device.Prn,
		Level:  in.Level,
		Source: in.Source,
		Rev:    in.Revision,
		Start:  int64(in.Offset),
		Page:   limit,
		Before: before,
		After:  after,
		Sort:   logs.Sorts{"-time-created"},
	})
	if err != nil {
		return nil, out, toolError(toolGetDeviceLogs, err)
	}

	for _, entry := range pager.Entries {
		if entry == nil {
			continue
		}
		message := entry.LogText
		if len(message) > maxLogMessage {
			message = strings.ToValidUTF8(message[:maxLogMessage], "") + " [truncated]"
		}
		out.Logs = append(out.Logs, logLine{
			Time:     formatTime(entry.TimeCreated),
			Level:    entry.LogLevel,
			Source:   entry.LogSource,
			Revision: entry.LogRev,
			Platform: entry.LogPlat,
			Message:  message,
		})
	}
	if int64(len(pager.Entries)) >= limit {
		out.NextOffset = in.Offset + len(pager.Entries)
	}
	return nil, out, nil
}

func parseTime(name, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC 3339 time such as 2026-01-31T12:00:00Z", name)
	}
	return &parsed, nil
}
