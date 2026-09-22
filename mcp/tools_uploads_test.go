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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/exports"
)

// fakeUploads stands in for exports.App; exports has its own tests.
type fakeUploads struct {
	uploads map[string]*exports.Upload
	fetched []string
}

func (f *fakeUploads) CreateUpload(_ context.Context, owner string) (*exports.Upload, string, time.Time, error) {
	upload := &exports.Upload{ID: "fresh", Owner: owner, Status: exports.UploadWaiting, ExpiresAt: time.Now().Add(time.Hour)}
	f.uploads[upload.ID] = upload
	return upload, "https://api.example.com/exports/uploads/token", time.Now().Add(30 * time.Minute), nil
}

func (f *fakeUploads) ImportFromURL(_ context.Context, owner, rawURL string) (*exports.Upload, error) {
	f.fetched = append(f.fetched, rawURL)
	return &exports.Upload{ID: "fetching", Owner: owner, Status: exports.UploadReceiving, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *fakeUploads) GetUpload(_ context.Context, owner, id string) (*exports.Upload, error) {
	upload, ok := f.uploads[id]
	if !ok || upload.Owner != owner {
		return nil, exports.ErrUploadNotFound
	}
	return upload, nil
}

func withUploads(f *toolFixture) *fakeUploads {
	uploads := &fakeUploads{uploads: map[string]*exports.Upload{
		"received": {
			ID: "received", Owner: ownerPrn, Status: exports.UploadReceived,
			State: map[string]interface{}{
				"#spec":             "pantavisor-service-system@1",
				"api/run.json":      map[string]interface{}{"name": "api"},
				"api/root.squashfs": "aaaa000000000000000000000000000000000000000000000000000000000000",
			},
			Objects: []string{"aaaa000000000000000000000000000000000000000000000000000000000000"},
			Stored:  1, ExpiresAt: time.Now().Add(time.Hour),
		},
		"incomplete": {
			ID: "incomplete", Owner: ownerPrn, Status: exports.UploadReceived,
			State:   map[string]interface{}{"#spec": "pantavisor-service-system@1", "db/root.squashfs": "bbbb000000000000000000000000000000000000000000000000000000000000"},
			Missing: []string{"db/root.squashfs"}, ExpiresAt: time.Now().Add(time.Hour),
		},
		"broken":  {ID: "broken", Owner: ownerPrn, Status: exports.UploadFailed, Error: "the archive has no json state", ExpiresAt: time.Now().Add(time.Hour)},
		"pending": {ID: "pending", Owner: ownerPrn, Status: exports.UploadReceiving, ExpiresAt: time.Now().Add(time.Hour)},
		"theirs":  {ID: "theirs", Owner: strangerPrn, Status: exports.UploadReceived, State: map[string]interface{}{"#spec": "x"}, ExpiresAt: time.Now().Add(time.Hour)},
	}}
	f.service.SetExportUploads(uploads)
	return uploads
}

func TestImportingAnExportAddsItsParts(t *testing.T) {
	f := newRevisionFixture(t)
	withUploads(f)

	plan, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "import_export", "upload_id": "received"})
	require.Empty(t, problem)
	assert.ElementsMatch(t, []string{"api/root.squashfs", "api/run.json"}, plan.Changes.Added)
	assert.Equal(t, []string{"api"}, plan.Changes.PartsTouched)
	assert.Empty(t, plan.Warnings)

	plan, problem = f.plan(t, "alpha_one", map[string]interface{}{"op": "import_export", "upload_id": "incomplete"})
	require.Empty(t, problem)
	require.NotEmpty(t, plan.Warnings)
	assert.Contains(t, plan.Warnings[0], "db/root.squashfs")

	for id, reason := range map[string]string{
		"broken":  "no json state",
		"pending": "still receiving",
		"theirs":  "no such upload",
		"":        "needs upload_id",
	} {
		_, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "import_export", "upload_id": id})
		assert.Contains(t, problem, reason, id)
	}
}

func TestUploadToolsSayWhatTheyGot(t *testing.T) {
	f := newRevisionFixture(t)
	uploads := withUploads(f)

	var link getExportUploadLinkOutput
	problem, _ := f.callAs(t, everyRevisionScope(), toolGetExportUploadLink, map[string]interface{}{}, &link)
	require.Empty(t, problem)
	assert.Equal(t, "fresh", link.UploadID)
	assert.NotEmpty(t, link.UploadURL)

	var fetched uploadSummary
	problem, _ = f.callAs(t, everyRevisionScope(), toolImportExportFromURL, map[string]interface{}{"url": "https://gitlab.com/a/export.tar.gz"}, &fetched)
	require.Empty(t, problem)
	assert.Equal(t, exports.UploadReceiving, fetched.Status)
	assert.Equal(t, []string{"https://gitlab.com/a/export.tar.gz"}, uploads.fetched)

	var received uploadSummary
	problem, _ = f.callAs(t, everyRevisionScope(), toolGetExportUpload, map[string]interface{}{"upload_id": "received"}, &received)
	require.Empty(t, problem)
	assert.Equal(t, exports.UploadReceived, received.Status)
	assert.Equal(t, 1, received.Objects)
	names := []string{}
	for _, part := range received.Parts {
		names = append(names, part.Name)
	}
	assert.ElementsMatch(t, []string{"#spec", "api"}, names)

	problem, _ = f.callAs(t, everyRevisionScope(), toolGetExportUpload, map[string]interface{}{"upload_id": "theirs"}, &received)
	assert.Contains(t, problem, "no such upload")
}

func TestUploadToolsWithoutUploads(t *testing.T) {
	f := newRevisionFixture(t)

	var link getExportUploadLinkOutput
	problem, _ := f.callAs(t, everyRevisionScope(), toolGetExportUploadLink, map[string]interface{}{}, &link)
	assert.Contains(t, problem, "not available")

	_, problem = f.plan(t, "alpha_one", map[string]interface{}{"op": "import_export", "upload_id": "x"})
	assert.Contains(t, problem, "not available")
}
