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
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/exports"
	"gitlab.com/pantacor/pantahub-base/trails/stateops"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// The tools in this file build new revisions the way the web app's pvtx
// editor does, and pvr: parts are removed, copied from another device or an
// older revision, rolled back, and inline JSON documents edited, all with the
// signatures that cover them handled as pvr handles them. A change is planned
// first and committed second, so what the user approves is exactly what the
// device is sent.
//
// Signatures are never made or checked here: the device verifies them. The
// plan says where a device that verifies them is likely to refuse the result.

// revisionInstructions tell an assistant how revisions are changed here.
const revisionInstructions = `New revisions are built like the web app's editor builds them.
get_revision_parts shows a revision's apps, BSP, configuration and signatures.
plan_revision prepares a change from the newest revision (remove, copy from
another device or revision, roll back, edit a JSON document, or import a pvr
export) without sending anything: its answer lists the changes and the
warnings. commit_revision takes that plan_id and sends the revision to the
device, which installs it; the device's status and logs report how that went.

Signatures are made with pvr or in CI, never here, and the device verifies
them. A plan warns where a device that verifies signatures would refuse it.

New builds arrive as pvr exports: get_export_upload_link for the user to
upload one, or import_export_from_url for a CI artifact; get_export_upload
tells when it is received, then plan_revision with import_export merges it.
get_export_link gives a short-lived download link to a revision's export.`

const (
	toolGetRevisionParts = "get_revision_parts"
	toolPlanRevision     = "plan_revision"
	toolCommitRevision   = "commit_revision"
	toolGetExportLink    = "get_export_link"

	maxPlanOperations = 20
	maxCommitMessage  = 500
	// maxDiffEntries bounds each list of a diff in a result; a rollback of a
	// whole state can touch hundreds of files.
	maxDiffEntries = 100

	exportLinkTTL = 10 * time.Minute

	opRemoveParts  = "remove_parts"
	opCopyParts    = "copy_parts"
	opRollback     = "rollback"
	opSetDocument  = "set_document"
	opDeleteFile   = "delete_file"
	revisionSource = "mcp"
)

// writeTrailScopes unlock posting a revision. The narrowest comes last; it is
// what a step-up challenge asks for.
var writeTrailScopes = utils.MarshalScopes([]utils.Scope{
	utils.Scopes.API,
	utils.Scopes.Trails,
	utils.Scopes.WriteTrails,
})

func init() {
	toolScopes[toolGetRevisionParts] = readTrailScopes
	// It stores a plan under the account, and a plan is only good for
	// commit_revision, which needs the same grant.
	toolScopes[toolPlanRevision] = writeTrailScopes
	toolScopes[toolCommitRevision] = writeTrailScopes
	// The same scopes GET /exports/:owner/:nick/:rev/:filename takes.
	toolScopes[toolGetExportLink] = readDeviceScopes
}

func (s *Service) registerRevisionTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetRevisionParts,
		Title: "Get revision parts",
		Description: "Describe a revision part by part, as the web app's editor shows it: each app or the BSP with its files, " +
			"its configuration overlay (_config/<name>), root documents, and every signature (_sigs/<name>.json) with the files it protects. " +
			"Use it before plan_revision to know what can be removed, copied or edited. The newest revision unless one is named.",
		Annotations: readOnly("Get revision parts"),
	}, s.getRevisionParts)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolPlanRevision,
		Title: "Plan a revision",
		Description: "Prepare a new revision of a device from its newest one, without sending anything to the device. " +
			"Operations apply in order: remove_parts (an app goes with its configuration and signature), " +
			"copy_parts (parts from another device, or from an older revision of this one, with the signatures that cover them), " +
			"rollback (the whole state of an older revision, or only some of its parts), " +
			"set_document (replace an inline JSON document such as <app>/run.json), delete_file, " +
			"and import_export (merge in a pvr export received through get_export_upload_link or import_export_from_url, as the web app's editor merges one). " +
			"Binaries cannot be edited, only copied with the part they belong to. " +
			"The answer lists every changed file and warns where a device that verifies signatures would refuse the result. " +
			"Nothing reaches the device until commit_revision is called with its plan_id. Plans expire after an hour.",
		Annotations: changes("Plan a revision"),
	}, s.planRevision)

	destructive := true
	closedWorld := false
	sdk.AddTool(server, &sdk.Tool{
		Name:  toolCommitRevision,
		Title: "Commit a planned revision",
		Description: "Send a planned revision to the device: exactly the state plan_revision showed becomes its next revision, " +
			"which the device downloads and installs. " +
			"It fails if the device got another revision since the plan was made; plan again then. " +
			"The device's status and its logs then report how the installation went.",
		Annotations: &sdk.ToolAnnotations{
			Title:           "Commit a planned revision",
			DestructiveHint: &destructive,
			IdempotentHint:  false,
			OpenWorldHint:   &closedWorld,
		},
	}, s.commitRevision)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetExportLink,
		Title: "Get export download link",
		Description: "Get a link that downloads a revision as a pvr export (a .tar.gz with the state and its objects), " +
			"optionally only some parts. The link works without signing in and expires after ten minutes. " +
			"Anyone holding it can download the export until it expires.",
		Annotations: readOnly("Get export download link"),
	}, s.getExportLink)
}

// revisionError turns an error of these tools into what the assistant should
// read: refusals explain themselves, anything else is logged and replaced.
func revisionError(tool string, err error) error {
	var validation validationError
	switch {
	case errors.As(err, &validation):
		return err
	case errors.Is(err, stateops.ErrInvalid):
		return errors.New(strings.TrimPrefix(err.Error(), stateops.ErrInvalid.Error()+": "))
	case errors.Is(err, errPlanGone), errors.Is(err, errRevisionMoved), errors.Is(err, errUploadsUnavailable):
		return err
	}
	return toolError(tool, err)
}

// get_revision_parts

type getRevisionPartsInput struct {
	Device   string `json:"device" jsonschema:"device id or nick"`
	Revision *int   `json:"revision,omitempty" jsonschema:"revision number; the newest if omitted"`
}

type getRevisionPartsOutput struct {
	Device     deviceSummary        `json:"device"`
	Revision   int                  `json:"revision"`
	Parts      []stateops.Part      `json:"parts"`
	Signatures []stateops.Signature `json:"signatures"`
}

func (s *Service) getRevisionParts(ctx context.Context, req *sdk.CallToolRequest, in getRevisionPartsInput) (*sdk.CallToolResult, getRevisionPartsOutput, error) {
	out := getRevisionPartsOutput{Parts: []stateops.Part{}, Signatures: []stateops.Signature{}}

	caller, err := authorize(req, toolGetRevisionParts)
	if err != nil {
		return nil, out, err
	}
	rev, err := revisionArg(in.Revision)
	if err != nil {
		return nil, out, err
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolGetRevisionParts, err)
	}
	step, err := s.store.getStep(ctx, caller.Prn, device.ID, rev, "")
	if err != nil {
		return nil, out, toolError(toolGetRevisionParts, err)
	}

	out.Device = summarize(device)
	out.Revision = step.Rev
	out.Parts = stateops.Parts(step.State)
	out.Signatures = stateops.Signatures(step.State)
	return nil, out, nil
}

func revisionArg(rev *int) (int, error) {
	if rev == nil {
		return -1, nil
	}
	if *rev < 0 {
		return 0, invalid("revision cannot be negative")
	}
	return *rev, nil
}

// plan_revision

type revisionOperation struct {
	Op         string      `json:"op" jsonschema:"remove_parts, copy_parts, rollback, set_document, delete_file or import_export"`
	Parts      []string    `json:"parts,omitempty" jsonschema:"part names as get_revision_parts lists them, for remove_parts, copy_parts and a partial rollback"`
	FromDevice string      `json:"from_device,omitempty" jsonschema:"copy_parts: device id or nick to copy from; this device if omitted"`
	FromRev    *int        `json:"from_revision,omitempty" jsonschema:"copy_parts: revision to copy from, the source's newest if omitted; rollback: the revision to go back to"`
	Path       string      `json:"path,omitempty" jsonschema:"set_document and delete_file: the file, e.g. myapp/run.json"`
	Value      interface{} `json:"value,omitempty" jsonschema:"set_document: the whole new JSON document"`
	UploadID   string      `json:"upload_id,omitempty" jsonschema:"import_export: upload_id of a received export upload"`
}

type planRevisionInput struct {
	Device     string              `json:"device" jsonschema:"device id or nick"`
	Operations []revisionOperation `json:"operations" jsonschema:"what to change, applied in order"`
}

type diffSummary struct {
	Added          []string `json:"added"`
	Removed        []string `json:"removed"`
	Changed        []string `json:"changed"`
	Omitted        int      `json:"omitted,omitempty" jsonschema:"files left out of the lists above, which are capped"`
	PartsTouched   []string `json:"parts_touched"`
	FilesInNewRevs int      `json:"files_in_new_revision"`
}

type planRevisionOutput struct {
	PlanID       string        `json:"plan_id"`
	Device       deviceSummary `json:"device"`
	BaseRevision int           `json:"base_revision" jsonschema:"the revision the plan starts from"`
	NewRevision  int           `json:"new_revision" jsonschema:"the revision number commit_revision will create"`
	Changes      diffSummary   `json:"changes"`
	Warnings     []string      `json:"warnings" jsonschema:"where a device verifying signatures is likely to refuse the revision"`
	ExpiresAt    string        `json:"expires_at"`
}

func (s *Service) planRevision(ctx context.Context, req *sdk.CallToolRequest, in planRevisionInput) (*sdk.CallToolResult, planRevisionOutput, error) {
	out := planRevisionOutput{Warnings: []string{}}

	caller, err := authorize(req, toolPlanRevision)
	if err != nil {
		return nil, out, err
	}
	if len(in.Operations) == 0 {
		return nil, out, invalid("name at least one operation")
	}
	if len(in.Operations) > maxPlanOperations {
		return nil, out, invalid("at most %d operations per plan", maxPlanOperations)
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolPlanRevision, err)
	}
	// Always the newest: the plan is committed as the revision after it.
	base, err := s.store.getStep(ctx, caller.Prn, device.ID, -1, "")
	if err != nil {
		return nil, out, toolError(toolPlanRevision, err)
	}

	state := base.State
	descriptions := []string{}
	opWarnings := []string{}
	for i, op := range in.Operations {
		var (
			next        map[string]interface{}
			description string
			warnings    []string
		)
		if strings.TrimSpace(op.Op) == opImportExport {
			next, description, warnings, err = s.importExport(ctx, caller.Prn, state, op.UploadID)
		} else {
			next, description, err = s.applyOperation(ctx, caller.Prn, device, state, op)
		}
		if err != nil {
			return nil, out, revisionError(toolPlanRevision, prefixed(i, err))
		}
		state = next
		descriptions = append(descriptions, description)
		opWarnings = append(opWarnings, warnings...)
	}

	if err := stateops.Validate(state); err != nil {
		return nil, out, revisionError(toolPlanRevision, err)
	}
	diff := stateops.Compare(base.State, state)
	if diff.Empty() {
		return nil, out, invalid("these operations change nothing in revision %d", base.Rev)
	}

	plan := &revisionPlan{
		Owner:       caller.Prn,
		Device:      device.ID,
		DeviceNick:  device.Nick,
		BaseRev:     base.Rev,
		State:       state,
		Diff:        diff,
		Warnings:    append(opWarnings, stateops.SignatureWarnings(base.State, state)...),
		Description: strings.Join(descriptions, "; "),
		ClientID:    caller.ClientID,
	}
	if err := s.store.savePlan(ctx, plan); err != nil {
		return nil, out, toolError(toolPlanRevision, err)
	}

	out.PlanID = plan.ID
	out.Device = summarize(device)
	out.BaseRevision = base.Rev
	out.NewRevision = base.Rev + 1
	out.Changes = summarizeDiff(diff, len(state))
	out.Warnings = plan.Warnings
	out.ExpiresAt = formatTime(plan.ExpiresAt)
	return nil, out, nil
}

// prefixed names the operation a refusal is about.
func prefixed(index int, err error) error {
	if errors.Is(err, stateops.ErrInvalid) {
		msg := strings.TrimPrefix(err.Error(), stateops.ErrInvalid.Error()+": ")
		return invalid("operation %d: %s", index+1, msg)
	}
	var validation validationError
	if errors.As(err, &validation) {
		return invalid("operation %d: %s", index+1, validation.msg)
	}
	return err
}

// applyOperation returns state changed by op, and a line saying what it did.
func (s *Service) applyOperation(ctx context.Context, owner string, device *devices.Device, state map[string]interface{}, op revisionOperation) (map[string]interface{}, string, error) {
	switch strings.TrimSpace(op.Op) {
	case opRemoveParts:
		next, err := stateops.RemoveParts(state, op.Parts)
		return next, "remove " + strings.Join(op.Parts, ", "), err

	case opCopyParts:
		source := device
		if ref := strings.TrimSpace(op.FromDevice); ref != "" {
			other, err := s.store.resolveDevice(ctx, owner, ref)
			if err != nil {
				if errors.Is(err, errNotFound) {
					return nil, "", invalid("there is no device %q among yours", ref)
				}
				return nil, "", err
			}
			source = other
		} else if op.FromRev == nil {
			return nil, "", invalid("copy_parts needs from_device, from_revision or both")
		}
		rev, err := revisionArg(op.FromRev)
		if err != nil {
			return nil, "", err
		}
		step, err := s.store.getStep(ctx, owner, source.ID, rev, "")
		if err != nil {
			if errors.Is(err, errNotFound) {
				return nil, "", invalid("%s has no such revision", source.Nick)
			}
			return nil, "", err
		}
		next, err := stateops.CopyParts(state, step.State, op.Parts)
		return next, "copy " + strings.Join(op.Parts, ", ") + " from " + source.Nick + " revision " + strconv.Itoa(step.Rev), err

	case opRollback:
		if op.FromRev == nil {
			return nil, "", invalid("rollback needs from_revision")
		}
		rev, err := revisionArg(op.FromRev)
		if err != nil {
			return nil, "", err
		}
		step, err := s.store.getStep(ctx, owner, device.ID, rev, "")
		if err != nil {
			if errors.Is(err, errNotFound) {
				return nil, "", invalid("%s has no revision %d", device.Nick, rev)
			}
			return nil, "", err
		}
		if len(op.Parts) == 0 {
			return step.State, "roll back to revision " + strconv.Itoa(step.Rev), nil
		}
		next, err := stateops.CopyParts(state, step.State, op.Parts)
		return next, "roll " + strings.Join(op.Parts, ", ") + " back to revision " + strconv.Itoa(step.Rev), err

	case opSetDocument:
		next, err := stateops.SetDocument(state, op.Path, op.Value)
		return next, "set " + op.Path, err

	case opDeleteFile:
		next, err := stateops.DeleteFile(state, op.Path)
		return next, "delete " + op.Path, err
	}
	return nil, "", invalid("unknown operation %q; use remove_parts, copy_parts, rollback, set_document, delete_file or import_export", op.Op)
}

func summarizeDiff(diff stateops.Diff, files int) diffSummary {
	summary := diffSummary{FilesInNewRevs: files}
	touched := map[string]bool{}
	for _, list := range [][]string{diff.Added, diff.Removed, diff.Changed} {
		for _, file := range list {
			touched[stateops.PartOf(file)] = true
		}
	}
	for part := range touched {
		summary.PartsTouched = append(summary.PartsTouched, part)
	}
	sort.Strings(summary.PartsTouched)

	capped := func(list []string) []string {
		if len(list) <= maxDiffEntries {
			return list
		}
		summary.Omitted += len(list) - maxDiffEntries
		return list[:maxDiffEntries]
	}
	summary.Added = capped(diff.Added)
	summary.Removed = capped(diff.Removed)
	summary.Changed = capped(diff.Changed)
	return summary
}

// commit_revision

type commitRevisionInput struct {
	PlanID  string `json:"plan_id" jsonschema:"plan_id from plan_revision"`
	Message string `json:"message" jsonschema:"what the revision does, shown in the device's history"`
}

type commitRevisionOutput struct {
	Revision revisionSummary `json:"revision"`
	Changes  diffSummary     `json:"changes"`
	Warnings []string        `json:"warnings"`
}

func (s *Service) commitRevision(ctx context.Context, req *sdk.CallToolRequest, in commitRevisionInput) (*sdk.CallToolResult, commitRevisionOutput, error) {
	out := commitRevisionOutput{Warnings: []string{}}

	caller, err := authorize(req, toolCommitRevision)
	if err != nil {
		return nil, out, err
	}
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return nil, out, invalid("give the revision a message saying what it does")
	}
	if len([]rune(message)) > maxCommitMessage {
		return nil, out, invalid("the message is longer than %d characters", maxCommitMessage)
	}

	plan, err := s.store.claimPlan(ctx, caller.Prn, strings.TrimSpace(in.PlanID))
	if err != nil {
		return nil, out, revisionError(toolCommitRevision, err)
	}

	meta := map[string]interface{}{
		"build":    "mcp: " + plan.Description,
		"source":   revisionSource,
		"mcp-plan": plan.ID,
	}
	if plan.ClientID != "" {
		meta["mcp-client"] = plan.ClientID
	}

	step, err := s.store.createRevision(ctx, caller.Prn, plan.Device, plan.BaseRev+1, plan.State, meta, message)
	if err != nil {
		// The plan is stale when the device moved on, and cannot be retried
		// then; any other failure may pass on a second try.
		if !errors.Is(err, errRevisionMoved) {
			s.store.releasePlan(ctx, caller.Prn, plan.ID)
		}
		return nil, out, revisionError(toolCommitRevision, err)
	}

	out.Revision = summarizeStep(step)
	out.Changes = summarizeDiff(plan.Diff, len(plan.State))
	out.Warnings = plan.Warnings
	return nil, out, nil
}

// get_export_link

type getExportLinkInput struct {
	Device   string   `json:"device" jsonschema:"device id or nick"`
	Revision *int     `json:"revision,omitempty" jsonschema:"revision number; the newest if omitted"`
	Parts    []string `json:"parts,omitempty" jsonschema:"only these parts; the whole revision if omitted"`
}

type getExportLinkOutput struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	Revision  int    `json:"revision"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Service) getExportLink(ctx context.Context, req *sdk.CallToolRequest, in getExportLinkInput) (*sdk.CallToolResult, getExportLinkOutput, error) {
	out := getExportLinkOutput{}

	caller, err := authorize(req, toolGetExportLink)
	if err != nil {
		return nil, out, err
	}
	if caller.Nick == "" {
		return nil, out, errors.New("this connection does not name its account; connect again")
	}
	rev, err := revisionArg(in.Revision)
	if err != nil {
		return nil, out, err
	}

	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, toolError(toolGetExportLink, err)
	}
	step, err := s.store.getStep(ctx, caller.Prn, device.ID, rev, "")
	if err != nil {
		return nil, out, toolError(toolGetExportLink, err)
	}
	parts := []string{}
	for _, part := range in.Parts {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}

	url, expires, err := exports.SignDownloadLink(exports.LinkRequest{
		OwnerPrn:   caller.Prn,
		OwnerNick:  caller.Nick,
		DeviceNick: device.Nick,
		Rev:        step.Rev,
		Parts:      parts,
	}, exportLinkTTL)
	if err != nil {
		return nil, out, toolError(toolGetExportLink, err)
	}

	out.URL = url
	out.Filename = device.Nick + "-" + strconv.Itoa(step.Rev) + ".tar.gz"
	out.Revision = step.Rev
	out.ExpiresAt = formatTime(expires)
	return nil, out, nil
}
