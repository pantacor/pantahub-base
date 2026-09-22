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
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gitlab.com/pantacor/pantahub-base/exports"
	"gitlab.com/pantacor/pantahub-base/trails/stateops"
)

// New content reaches a revision as a pvr export: uploaded by the user
// through a link, or fetched by the server from a URL (a CI artifact), then
// merged in by the import_export operation of plan_revision. Exports are
// built and signed elsewhere, with pvr or in CI; nothing here signs.

const (
	toolGetExportUploadLink = "get_export_upload_link"
	toolImportExportFromURL = "import_export_from_url"
	toolGetExportUpload     = "get_export_upload"

	opImportExport = "import_export"
)

// exportUploads is what the endpoint needs of exports.App.
type exportUploads interface {
	CreateUpload(ctx context.Context, owner string) (*exports.Upload, string, time.Time, error)
	ImportFromURL(ctx context.Context, owner, rawURL string) (*exports.Upload, error)
	GetUpload(ctx context.Context, owner, id string) (*exports.Upload, error)
}

// SetExportUploads lets the endpoint take pvr exports into the account.
// Without it the upload tools answer that uploads are not available.
func (s *Service) SetExportUploads(uploads exportUploads) {
	s.uploads = uploads
}

var errUploadsUnavailable = errors.New("export uploads are not available on this server")

func init() {
	// Both put objects into the account's storage.
	toolScopes[toolGetExportUploadLink] = writeTrailScopes
	toolScopes[toolImportExportFromURL] = writeTrailScopes
	toolScopes[toolGetExportUpload] = readTrailScopes
}

func (s *Service) registerUploadTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetExportUploadLink,
		Title: "Get export upload link",
		Description: "Get a single-use link that takes one pvr export (the .tar.gz `pvr export` writes) into the account, " +
			"which the user uploads the file to with any HTTP client (curl -T export.tar.gz '<upload_url>'). " +
			"The link expires after thirty minutes. Its objects are stored in the account, and the import_export operation of " +
			"plan_revision then puts the export into a revision. get_export_upload reports whether it arrived.",
		Annotations: changes("Get export upload link"),
	}, s.getExportUploadLink)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolImportExportFromURL,
		Title: "Import export from URL",
		Description: "Have the server fetch a pvr export from an https URL, such as a CI artifact, and take it into the account. " +
			"Only hosts this server allows can be fetched from. The fetch runs in the background; " +
			"get_export_upload reports when it has been received, and the import_export operation of plan_revision merges it.",
		Annotations: changes("Import export from URL"),
	}, s.importExportFromURL)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolGetExportUpload,
		Title: "Get export upload",
		Description: "Tell how an export upload or import is going, and once received what it holds: its parts and signatures, " +
			"how many objects were new to the account, and any object its state names that is missing.",
		Annotations: readOnly("Get export upload"),
	}, s.getExportUpload)
}

func uploadError(tool string, err error) error {
	switch {
	case errors.Is(err, exports.ErrUploadNotFound):
		return errors.New("no such upload: it expired, or was never made")
	case errors.Is(err, exports.ErrImportDisabled), errors.Is(err, errUploadsUnavailable):
		return err
	}
	var validation validationError
	if errors.As(err, &validation) {
		return err
	}
	return toolError(tool, err)
}

// get_export_upload_link

type getExportUploadLinkOutput struct {
	UploadID  string `json:"upload_id"`
	UploadURL string `json:"upload_url" jsonschema:"PUT the archive here, once; anyone holding it can until it expires"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Service) getExportUploadLink(ctx context.Context, req *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, getExportUploadLinkOutput, error) {
	out := getExportUploadLinkOutput{}
	caller, err := authorize(req, toolGetExportUploadLink)
	if err != nil {
		return nil, out, err
	}
	if s.uploads == nil {
		return nil, out, errUploadsUnavailable
	}
	upload, link, expires, err := s.uploads.CreateUpload(ctx, caller.Prn)
	if err != nil {
		return nil, out, uploadError(toolGetExportUploadLink, err)
	}
	out.UploadID = upload.ID
	out.UploadURL = link
	out.ExpiresAt = formatTime(expires)
	return nil, out, nil
}

// import_export_from_url

type importExportFromURLInput struct {
	URL string `json:"url" jsonschema:"https URL of a pvr export (.tar.gz)"`
}

type uploadSummary struct {
	UploadID   string               `json:"upload_id"`
	Status     string               `json:"status" jsonschema:"waiting, receiving, received or failed"`
	Error      string               `json:"error,omitempty"`
	Size       int64                `json:"size,omitempty"`
	Objects    int                  `json:"objects,omitempty"`
	Stored     int                  `json:"stored,omitempty" jsonschema:"objects that were new to the account"`
	Missing    []string             `json:"missing,omitempty" jsonschema:"files whose object neither the export nor the account has"`
	Parts      []stateops.Part      `json:"parts,omitempty"`
	Signatures []stateops.Signature `json:"signatures,omitempty"`
	ExpiresAt  string               `json:"expires_at"`
}

func summarizeUpload(upload *exports.Upload) uploadSummary {
	summary := uploadSummary{
		UploadID:  upload.ID,
		Status:    upload.Status,
		Error:     upload.Error,
		Size:      upload.Size,
		Objects:   len(upload.Objects),
		Stored:    upload.Stored,
		Missing:   upload.Missing,
		ExpiresAt: formatTime(upload.ExpiresAt),
	}
	if upload.Status == exports.UploadReceived && upload.State != nil {
		summary.Parts = stateops.Parts(upload.State)
		summary.Signatures = stateops.Signatures(upload.State)
	}
	return summary
}

func (s *Service) importExportFromURL(ctx context.Context, req *sdk.CallToolRequest, in importExportFromURLInput) (*sdk.CallToolResult, uploadSummary, error) {
	out := uploadSummary{}
	caller, err := authorize(req, toolImportExportFromURL)
	if err != nil {
		return nil, out, err
	}
	if s.uploads == nil {
		return nil, out, errUploadsUnavailable
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, out, invalid("url is required")
	}
	upload, err := s.uploads.ImportFromURL(ctx, caller.Prn, in.URL)
	if err != nil {
		// The URL checks explain themselves.
		if !errors.Is(err, exports.ErrImportDisabled) && !strings.Contains(err.Error(), "database") {
			return nil, out, invalid("%s", err.Error())
		}
		return nil, out, uploadError(toolImportExportFromURL, err)
	}
	return nil, summarizeUpload(upload), nil
}

// get_export_upload

type getExportUploadInput struct {
	UploadID string `json:"upload_id"`
}

func (s *Service) getExportUpload(ctx context.Context, req *sdk.CallToolRequest, in getExportUploadInput) (*sdk.CallToolResult, uploadSummary, error) {
	out := uploadSummary{}
	caller, err := authorize(req, toolGetExportUpload)
	if err != nil {
		return nil, out, err
	}
	if s.uploads == nil {
		return nil, out, errUploadsUnavailable
	}
	upload, err := s.uploads.GetUpload(ctx, caller.Prn, strings.TrimSpace(in.UploadID))
	if err != nil {
		return nil, out, uploadError(toolGetExportUpload, err)
	}
	return nil, summarizeUpload(upload), nil
}

// importExport is the import_export operation of plan_revision.
func (s *Service) importExport(ctx context.Context, owner string, state map[string]interface{}, uploadID string) (map[string]interface{}, string, []string, error) {
	if s.uploads == nil {
		return nil, "", nil, errUploadsUnavailable
	}
	if strings.TrimSpace(uploadID) == "" {
		return nil, "", nil, invalid("import_export needs upload_id")
	}
	upload, err := s.uploads.GetUpload(ctx, owner, strings.TrimSpace(uploadID))
	if err != nil {
		if errors.Is(err, exports.ErrUploadNotFound) {
			return nil, "", nil, invalid("no such upload: it expired, or was never made")
		}
		return nil, "", nil, err
	}
	switch upload.Status {
	case exports.UploadReceived:
	case exports.UploadFailed:
		return nil, "", nil, invalid("that upload failed: %s", upload.Error)
	default:
		return nil, "", nil, invalid("that upload is still %s; check get_export_upload", upload.Status)
	}

	next, err := stateops.Import(state, upload.State)
	if err != nil {
		return nil, "", nil, err
	}
	warnings := []string{}
	if len(upload.Missing) > 0 {
		warnings = append(warnings, "the export names objects neither it nor the account has ("+
			strings.Join(upload.Missing, ", ")+"); committing will be refused")
	}
	return next, "import export " + upload.ID, warnings, nil
}
