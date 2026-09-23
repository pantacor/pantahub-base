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
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/trails/stateops"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
)

// newRevisionFixture is the tool fixture plus the trail documents revisions
// are posted to, and a revision 3 of alpha_one with an app in it.
func newRevisionFixture(t *testing.T) *toolFixture {
	t.Helper()
	f := newToolFixture(t)
	db := f.service.store.mongoClient.Database(utils.MongoDb)
	ctx := context.Background()

	_, err := db.Collection(trailsCollection).InsertMany(ctx, []interface{}{
		bson.M{"_id": f.own, "owner": ownerPrn, "device": devicePrnPrefix + f.own.Hex(), "garbage": false},
		bson.M{"_id": f.foreign, "owner": strangerPrn, "device": devicePrnPrefix + f.foreign.Hex(), "garbage": false},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = db.Collection(stepsCollection).InsertOne(ctx, bson.M{
		"_id": fmt.Sprintf("%s-3", f.own.Hex()), "trail-id": f.own, "owner": ownerPrn,
		"device": devicePrnPrefix + f.own.Hex(), "rev": 3, "commit-msg": "rev 3",
		"garbage": false, "step-time": now, "progress-time": now,
		"state": quoted(map[string]interface{}{
			"#spec":        "pantavisor-service-system@1",
			"bsp/run.json": map[string]interface{}{"initrd": "pantavisor"},
			"web/run.json": map[string]interface{}{"name": "web", "args": []interface{}{"--port", "80"}},
			"storage.json": map[string]interface{}{"disks": []interface{}{}},
		}),
		"progress": bson.M{"status": "DONE"},
	})
	require.NoError(t, err)
	return f
}

func everyRevisionScope() []string {
	return append(supportedScopes(), writeTrailScopes...)
}

func (f *toolFixture) plan(t *testing.T, device string, operations ...map[string]interface{}) (planRevisionOutput, string) {
	t.Helper()
	var out planRevisionOutput
	problem, _ := f.callAs(t, everyRevisionScope(), toolPlanRevision, map[string]interface{}{
		"device": device, "operations": operations,
	}, &out)
	return out, problem
}

func storedStep(t *testing.T, f *toolFixture, rev int) *trailmodels.Step {
	t.Helper()
	step, err := f.service.store.getStep(context.Background(), ownerPrn, f.own, rev, "")
	require.NoError(t, err)
	return step
}

func TestRevisionPartsNameAppsAndDocuments(t *testing.T) {
	f := newRevisionFixture(t)

	var out getRevisionPartsOutput
	problem, _ := f.callAs(t, everyRevisionScope(), toolGetRevisionParts, map[string]interface{}{"device": "alpha_one"}, &out)
	require.Empty(t, problem)
	assert.Equal(t, 3, out.Revision, "the newest by default")

	names := map[string]string{}
	for _, part := range out.Parts {
		names[part.Name] = part.Kind
	}
	assert.Equal(t, map[string]string{"#spec": "document", "bsp": "app", "web": "app", "storage.json": "document"}, names)
	assert.Empty(t, out.Signatures)
}

func TestPlanThenCommitPostsExactlyThePlannedState(t *testing.T) {
	f := newRevisionFixture(t)

	plan, problem := f.plan(t, "alpha_one", map[string]interface{}{
		"op": "set_document", "path": "web/run.json",
		"value": map[string]interface{}{"name": "web", "args": []interface{}{"--port", "8080"}},
	})
	require.Empty(t, problem)
	assert.Equal(t, 3, plan.BaseRevision)
	assert.Equal(t, 4, plan.NewRevision)
	assert.Equal(t, []string{"web/run.json"}, plan.Changes.Changed)
	assert.Equal(t, []string{"web"}, plan.Changes.PartsTouched)
	assert.NotEmpty(t, plan.PlanID)

	// Planning sends nothing.
	_, err := f.service.store.getStep(context.Background(), ownerPrn, f.own, 4, "")
	assert.ErrorIs(t, err, errNotFound)

	var committed commitRevisionOutput
	problem, _ = f.callAs(t, everyRevisionScope(), toolCommitRevision, map[string]interface{}{
		"plan_id": plan.PlanID, "message": "listen on 8080",
	}, &committed)
	require.Empty(t, problem)
	assert.Equal(t, 4, committed.Revision.Revision)
	assert.Equal(t, "NEW", committed.Revision.Status)

	step := storedStep(t, f, 4)
	assert.Equal(t, "listen on 8080", step.CommitMsg)
	assert.Equal(t, []interface{}{"--port", "8080"}, step.State["web/run.json"].(map[string]interface{})["args"])
	assert.Equal(t, map[string]interface{}{"initrd": "pantavisor"}, step.State["bsp/run.json"], "the rest is untouched")
	assert.Equal(t, "mcp", step.Meta["source"])
	assert.Equal(t, plan.PlanID, step.Meta["mcp-plan"])
	assert.NotEmpty(t, step.StateSha, "created through the REST code, which computes it")

	// A plan is committed once.
	problem, _ = f.callAs(t, everyRevisionScope(), toolCommitRevision, map[string]interface{}{
		"plan_id": plan.PlanID, "message": "again",
	}, &committed)
	assert.Contains(t, problem, "no such plan")
}

func TestAStalePlanIsRefused(t *testing.T) {
	f := newRevisionFixture(t)

	plan, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "remove_parts", "parts": []string{"web"}})
	require.Empty(t, problem)
	assert.Equal(t, []string{"web/run.json"}, plan.Changes.Removed)

	// Somebody posts revision 4 in the meantime.
	other, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "delete_file", "path": "storage.json"})
	require.Empty(t, problem)
	var committed commitRevisionOutput
	problem, _ = f.callAs(t, everyRevisionScope(), toolCommitRevision, map[string]interface{}{"plan_id": other.PlanID, "message": "first"}, &committed)
	require.Empty(t, problem)

	problem, _ = f.callAs(t, everyRevisionScope(), toolCommitRevision, map[string]interface{}{"plan_id": plan.PlanID, "message": "second"}, &committed)
	assert.Contains(t, problem, "new revision since this plan")
	assert.Contains(t, storedStep(t, f, 4).State, "web/run.json", "revision 4 is the one that got there first")
}

func TestPlansOnlyReachTheCallersDevices(t *testing.T) {
	f := newRevisionFixture(t)

	_, problem := f.plan(t, "alpha_foreign", map[string]interface{}{"op": "delete_file", "path": "bsp/run.json"})
	assert.NotEmpty(t, problem)

	_, problem = f.plan(t, "alpha_one", map[string]interface{}{"op": "copy_parts", "from_device": "alpha_foreign", "parts": []string{"bsp"}})
	assert.Contains(t, problem, "no device")

	// And somebody else's plan cannot be committed.
	plan, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "delete_file", "path": "storage.json"})
	require.Empty(t, problem)
	db := f.service.store.mongoClient.Database(utils.MongoDb)
	_, err := db.Collection(plansCollection).UpdateOne(context.Background(), bson.M{"_id": plan.PlanID}, bson.M{"$set": bson.M{"owner": strangerPrn}})
	require.NoError(t, err)
	var committed commitRevisionOutput
	problem, _ = f.callAs(t, everyRevisionScope(), toolCommitRevision, map[string]interface{}{"plan_id": plan.PlanID, "message": "x"}, &committed)
	assert.Contains(t, problem, "no such plan")
}

func TestCommittingNeedsTheWriteScope(t *testing.T) {
	f := newRevisionFixture(t)

	plan, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "delete_file", "path": "storage.json"})
	require.Empty(t, problem)

	// A read-only grant is turned away before the tool runs, with the step-up
	// challenge that asks the user for trails.write.
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{"plan_id":%q,"message":"x"}}}`,
		toolCommitRevision, plan.PlanID)
	readOnly := utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadTrails})
	rec := rpc(t, f.service, sign(t, testKey, userClaims(readOnly...)), body)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "trails.write")

	_, err := f.service.store.getStep(context.Background(), ownerPrn, f.own, 4, "")
	assert.ErrorIs(t, err, errNotFound)
}

func TestPlanRefusals(t *testing.T) {
	f := newRevisionFixture(t)

	for name, op := range map[string]map[string]interface{}{
		"unknown operation":      {"op": "format_disk"},
		"missing part":           {"op": "remove_parts", "parts": []string{"nope"}},
		"a binary edited inline": {"op": "set_document", "path": "web/root.squashfs", "value": map[string]interface{}{}},
		"a signature by hand":    {"op": "set_document", "path": "_sigs/web.json", "value": map[string]interface{}{}},
		"copy with no source":    {"op": "copy_parts", "parts": []string{"web"}},
		"rollback to nowhere":    {"op": "rollback"},
		"no such revision":       {"op": "rollback", "from_revision": 99},
		"the spec removed":       {"op": "delete_file", "path": "#spec"},
		"nothing changes":        {"op": "set_document", "path": "storage.json", "value": map[string]interface{}{"disks": []interface{}{}}},
	} {
		_, problem := f.plan(t, "alpha_one", op)
		assert.NotEmpty(t, problem, name)
		assert.NotContains(t, problem, "internal error", name)
	}

	_, problem := f.plan(t, "alpha_one")
	assert.NotEmpty(t, problem, "no operations")
}

func TestRollbackOfOnePart(t *testing.T) {
	f := newRevisionFixture(t)

	// Revision 2 has no web app and a bare bsp: rolling bsp back to it leaves web.
	plan, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "rollback", "from_revision": 2, "parts": []string{"bsp"}})
	require.Empty(t, problem)
	assert.Equal(t, []string{"bsp/run.json"}, plan.Changes.Changed)
	assert.Empty(t, plan.Changes.Removed)

	whole, problem := f.plan(t, "alpha_one", map[string]interface{}{"op": "rollback", "from_revision": 2})
	require.Empty(t, problem)
	assert.ElementsMatch(t, []string{"storage.json", "web/run.json"}, whole.Changes.Removed)
}

func TestExportLinkNamesOneExport(t *testing.T) {
	f := newRevisionFixture(t)

	var out getExportLinkOutput
	problem, _ := f.callAs(t, supportedScopes(), toolGetExportLink, map[string]interface{}{
		"device": "alpha_one", "revision": 2, "parts": []string{"bsp"},
	}, &out)
	require.Empty(t, problem)
	assert.Equal(t, 2, out.Revision)
	assert.Equal(t, "alpha_one-2.tar.gz", out.Filename)

	link, err := url.Parse(out.URL)
	require.NoError(t, err)
	assert.True(t, strings.Contains(link.Path, "/exports/links/"), link.Path)
	assert.True(t, strings.HasSuffix(link.Path, "/alpha_one-2.tar.gz"))

	expires, err := time.Parse(time.RFC3339, out.ExpiresAt)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(exportLinkTTL), expires, time.Minute)

	problem, _ = f.callAs(t, supportedScopes(), toolGetExportLink, map[string]interface{}{"device": "alpha_foreign"}, &out)
	assert.NotEmpty(t, problem)
}

func TestSignedStatesKeepTheirWarningsThroughThePlan(t *testing.T) {
	// The plan carries stateops' warnings; stateops has the signature cases.
	before := map[string]interface{}{"#spec": "pantavisor-service-system@1"}
	after := map[string]interface{}{"#spec": "pantavisor-service-system@1", "x.json": map[string]interface{}{}}
	assert.Empty(t, stateops.SignatureWarnings(before, after), "an unsigned device gets no signature warnings")
}
