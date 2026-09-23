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
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// envTestMongo points the tool tests at a disposable MongoDB, for example
// mongodb://127.0.0.1:27017. They are skipped without it.
const envTestMongo = "PANTAHUB_MCP_TEST_MONGO"

const (
	ownerPrn    = "prn:pantahub.com:auth:/user1"
	strangerPrn = "prn:pantahub.com:auth:/user2"
)

// toolFixture is a service over a scratch database holding three devices of
// user1 (one of them deleted) and one of user2.
type toolFixture struct {
	service *Service
	own     primitive.ObjectID
	foreign primitive.ObjectID
}

const devicePrnPrefix = "prn:::devices:/"

func newToolFixture(t *testing.T) *toolFixture {
	t.Helper()
	uri := os.Getenv(envTestMongo)
	if uri == "" {
		t.Skip(envTestMongo + " is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))

	previousDb := utils.MongoDb
	utils.MongoDb = fmt.Sprintf("mcp_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		client.Database(utils.MongoDb).Drop(context.Background())
		utils.MongoDb = previousDb
		client.Disconnect(context.Background())
	})

	t.Setenv(EnvMcpAllowAPITokens, "false")
	service, err := New(client, &logs.App{})
	require.NoError(t, err)
	standInForConnections(service)

	fixture := &toolFixture{service: service, own: primitive.NewObjectID(), foreign: primitive.NewObjectID()}
	now := time.Now().UTC()

	device := func(id primitive.ObjectID, nick, owner string, garbage bool) bson.M {
		return bson.M{
			"_id": id, "prn": devicePrnPrefix + id.Hex(), "nick": nick, "owner": owner,
			"garbage": garbage, "ispublic": false, "timecreated": now, "timemodified": now,
			"meta-modified": now, "secret_hash": "never-returned", "challenge": "never-returned",
			"user-meta":   quoted(map[string]interface{}{"site": "lab"}),
			"device-meta": quoted(map[string]interface{}{"pantavisor.version": "019"}),
		}
	}
	_, err = client.Database(utils.MongoDb).Collection(devicesCollection).InsertMany(ctx, []interface{}{
		device(fixture.own, "alpha_one", ownerPrn, false),
		device(primitive.NewObjectID(), "beta_two", ownerPrn, false),
		device(primitive.NewObjectID(), "alpha_deleted", ownerPrn, true),
		device(fixture.foreign, "alpha_foreign", strangerPrn, false),
	})
	require.NoError(t, err)

	step := func(trail primitive.ObjectID, owner string, rev int, status string) bson.M {
		return bson.M{
			"_id": fmt.Sprintf("%s-%d", trail.Hex(), rev), "trail-id": trail, "owner": owner,
			"device": devicePrnPrefix + trail.Hex(), "rev": rev, "commit-msg": fmt.Sprintf("rev %d", rev),
			"garbage": false, "step-time": now, "progress-time": now, "statesha": "sha",
			"state": quoted(map[string]interface{}{
				"#spec": "pantavisor-service-system@1", "bsp/run.json": map[string]interface{}{},
			}),
			"progress":     bson.M{"status": status, "statusmsg": status + " message", "progress": 50, "logs": "noisy"},
			"progress-log": []bson.M{{"time": now, "source": "device", "status": status, "progress": 50, "statusmsg": status + " message"}},
		}
	}
	_, err = client.Database(utils.MongoDb).Collection(stepsCollection).InsertMany(ctx, []interface{}{
		step(fixture.own, ownerPrn, 0, "DONE"),
		step(fixture.own, ownerPrn, 1, "DONE"),
		step(fixture.own, ownerPrn, 2, "ERROR"),
		step(fixture.foreign, strangerPrn, 0, "DONE"),
	})
	require.NoError(t, err)

	return fixture
}

// quoted stores a map the way the API does: Mongo keys cannot hold dots.
func quoted(m map[string]interface{}) map[string]interface{} {
	return utils.BsonQuoteMap(&m)
}

// call runs a tool as user1 with every read scope and returns its structured
// result, or the error text when the tool reported one.
func (f *toolFixture) call(t *testing.T, tool string, args map[string]interface{}, out interface{}) string {
	t.Helper()

	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": tool, "arguments": args},
	})
	require.NoError(t, err)

	rec := rpc(t, f.service, sign(t, testKey, userClaims(devicesScope, trailsScope)), string(body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
			Content           []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response), rec.Body.String())

	if response.Result.IsError {
		require.NotEmpty(t, response.Result.Content)
		return response.Result.Content[0].Text
	}
	require.NotEmpty(t, response.Result.Content, "a text rendering has to accompany the structured result")
	require.NoError(t, json.Unmarshal(response.Result.StructuredContent, out), rec.Body.String())
	return ""
}

func TestListDevicesIsOwnerScopedAndPaged(t *testing.T) {
	fixture := newToolFixture(t)

	var page listDevicesOutput
	require.Empty(t, fixture.call(t, toolListDevices, map[string]interface{}{"limit": 1}, &page))
	require.Len(t, page.Devices, 1)
	assert.Equal(t, "alpha_one", page.Devices[0].Nick)
	require.NotEmpty(t, page.NextCursor)

	var rest listDevicesOutput
	require.Empty(t, fixture.call(t, toolListDevices, map[string]interface{}{"cursor": page.NextCursor}, &rest))
	require.Len(t, rest.Devices, 1, "deleted and foreign devices are not listed")
	assert.Equal(t, "beta_two", rest.Devices[0].Nick)
	assert.Empty(t, rest.NextCursor)

	var filtered listDevicesOutput
	require.Empty(t, fixture.call(t, toolListDevices, map[string]interface{}{"nick_prefix": "ALPHA"}, &filtered))
	require.Len(t, filtered.Devices, 1)
	assert.Equal(t, "alpha_one", filtered.Devices[0].Nick)

	// A prefix is matched literally, never as a pattern.
	var literal listDevicesOutput
	require.Empty(t, fixture.call(t, toolListDevices, map[string]interface{}{"nick_prefix": ".*"}, &literal))
	assert.Empty(t, literal.Devices)

	assert.Contains(t, fixture.call(t, toolListDevices, map[string]interface{}{"cursor": "nonsense"}, &page), "cursor")
}

func TestGetDeviceReturnsMetadataButNoCredentials(t *testing.T) {
	fixture := newToolFixture(t)

	for _, ref := range []string{"alpha_one", fixture.own.Hex(), devicePrnPrefix + fixture.own.Hex()} {
		var raw map[string]interface{}
		require.Empty(t, fixture.call(t, toolGetDevice, map[string]interface{}{"device": ref}, &raw), ref)

		assert.Equal(t, fixture.own.Hex(), raw["id"], ref)
		assert.Equal(t, map[string]interface{}{"site": "lab"}, raw["user_meta"])
		assert.Equal(t, map[string]interface{}{"pantavisor.version": "019"}, raw["device_meta"], "stored keys are unquoted")

		encoded, _ := json.Marshal(raw)
		assert.NotContains(t, string(encoded), "never-returned")
	}
}

func TestForeignAndDeletedDevicesLookTheSameAsMissingOnes(t *testing.T) {
	fixture := newToolFixture(t)

	var unused map[string]interface{}
	missing := fixture.call(t, toolGetDevice, map[string]interface{}{"device": "no_such_device"}, &unused)
	require.NotEmpty(t, missing)

	for _, ref := range []string{fixture.foreign.Hex(), "alpha_foreign", "alpha_deleted"} {
		for _, tool := range []string{toolGetDevice, toolGetDeviceStatus, toolListRevisions, toolGetRevision} {
			assert.Equal(t, missing, fixture.call(t, tool, map[string]interface{}{"device": ref}, &unused), tool+" "+ref)
		}
	}
}

func TestDeviceStatusReportsLatestAndLastDone(t *testing.T) {
	fixture := newToolFixture(t)

	var status getDeviceStatusOutput
	require.Empty(t, fixture.call(t, toolGetDeviceStatus, map[string]interface{}{"device": "alpha_one"}, &status))

	require.NotNil(t, status.LatestRevision)
	assert.Equal(t, 2, status.LatestRevision.Revision)
	assert.Equal(t, "ERROR", status.LatestRevision.Status)
	assert.Equal(t, "ERROR message", status.LatestRevision.StatusMsg)
	require.NotNil(t, status.LastDone)
	assert.Equal(t, 1, status.LastDone.Revision)
	assert.False(t, status.UpToDate)

	// A device without any revision is reported, not an error.
	var bare getDeviceStatusOutput
	require.Empty(t, fixture.call(t, toolGetDeviceStatus, map[string]interface{}{"device": "beta_two"}, &bare))
	assert.Nil(t, bare.LatestRevision)
	assert.False(t, bare.UpToDate)
}

func TestRevisionsArePagedNewestFirst(t *testing.T) {
	fixture := newToolFixture(t)

	var page listRevisionsOutput
	require.Empty(t, fixture.call(t, toolListRevisions, map[string]interface{}{"device": "alpha_one", "limit": 2}, &page))
	require.Len(t, page.Revisions, 2)
	assert.Equal(t, 2, page.Revisions[0].Revision)
	assert.Equal(t, 1, page.Revisions[1].Revision)
	assert.Equal(t, 1, page.NextBeforeRevision)

	var rest listRevisionsOutput
	require.Empty(t, fixture.call(t, toolListRevisions,
		map[string]interface{}{"device": "alpha_one", "before_revision": page.NextBeforeRevision}, &rest))
	require.Len(t, rest.Revisions, 1)
	assert.Equal(t, 0, rest.Revisions[0].Revision)
	assert.Zero(t, rest.NextBeforeRevision)
}

func TestGetRevision(t *testing.T) {
	fixture := newToolFixture(t)

	var latest getRevisionOutput
	require.Empty(t, fixture.call(t, toolGetRevision, map[string]interface{}{"device": "alpha_one"}, &latest))
	assert.Equal(t, 2, latest.Revision)
	assert.Equal(t, []string{"#spec", "bsp/run.json"}, latest.StateFiles)
	assert.Nil(t, latest.State, "the state document is opt-in")
	assert.Equal(t, "noisy", latest.ProgressLogs, "what the device said about the failure")
	require.Len(t, latest.ProgressLog, 1)
	assert.Equal(t, "device", latest.ProgressLog[0].Source)
	assert.Equal(t, "ERROR", latest.ProgressLog[0].Status)

	// Revision 0 is a real revision, not "unset".
	var first getRevisionOutput
	require.Empty(t, fixture.call(t, toolGetRevision,
		map[string]interface{}{"device": "alpha_one", "revision": 0, "include_state": true}, &first))
	assert.Equal(t, 0, first.Revision)
	assert.Contains(t, first.State, "bsp/run.json")

	var unused getRevisionOutput
	assert.NotEmpty(t, fixture.call(t, toolGetRevision, map[string]interface{}{"device": "alpha_one", "revision": 9}, &unused))
}
