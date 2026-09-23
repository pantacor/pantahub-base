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

package devices

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type joinTokenFixture struct {
	app              *App
	active, disabled primitive.ObjectID
	created          time.Time
}

// newJoinTokenFixture stores an active and a disabled token of testOwnerPrn,
// both with a dotted default-user-meta key, quoted as POST stores it.
func newJoinTokenFixture(t *testing.T) *joinTokenFixture {
	t.Helper()
	client := newTestClient(t)
	f := &joinTokenFixture{
		app:      &App{mongoClient: client},
		active:   primitive.NewObjectID(),
		disabled: primitive.NewObjectID(),
		created:  time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond),
	}
	meta := map[string]interface{}{"fleet.group": "lab"}
	token := func(id primitive.ObjectID, nick string, disabled bool) bson.M {
		return bson.M{
			"_id": id, "nick": nick, "owner": testOwnerPrn, "disabled": disabled,
			"tokensha": []byte("hash"), "defaultusermeta": utils.BsonQuoteMap(&meta),
			"timecreated": f.created, "timemodified": f.created,
		}
	}
	_, err := client.Database(utils.MongoDb).Collection(joinTokensCollection).InsertMany(context.Background(),
		[]interface{}{token(f.active, "factory_line", false), token(f.disabled, "retired", true)})
	require.NoError(t, err)
	return f
}

func (f *joinTokenFixture) stored(t *testing.T, id primitive.ObjectID) bson.M {
	t.Helper()
	doc := bson.M{}
	require.NoError(t, f.app.mongoClient.Database(utils.MongoDb).Collection(joinTokensCollection).
		FindOne(context.Background(), bson.M{"_id": id}).Decode(&doc))
	return doc
}

func tokenParam(id primitive.ObjectID) echo.PathValue {
	return echo.PathValue{Name: "id", Value: id.Hex()}
}

func TestJoinTokensAreReadUnquotedAndOnlyWhileActive(t *testing.T) {
	f := newJoinTokenFixture(t)

	rec := testRequest(t, f.app.handleGetTokens, testOwnerPrn, http.MethodGet, "/devices/tokens", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	listed := []utils.PantahubDevicesJoinToken{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, map[string]interface{}{"fleet.group": "lab"}, listed[0].DefaultUserMeta)
	assert.Empty(t, listed[0].TokenSha)

	rec = testRequest(t, f.app.handleGetToken, testOwnerPrn, http.MethodGet, "/devices/tokens/x", nil, tokenParam(f.active))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"fleet.group":"lab"`)
	assert.NotContains(t, rec.Body.String(), "token-sha")

	rec = testRequest(t, f.app.handleGetToken, testOwnerPrn, http.MethodGet, "/devices/tokens/x", nil, tokenParam(f.disabled))
	assert.Equal(t, http.StatusNotFound, rec.Code, "a disabled token is gone")

	rec = testRequest(t, f.app.handleGetToken, testStrangerPrn, http.MethodGet, "/devices/tokens/x", nil, tokenParam(f.active))
	assert.Equal(t, http.StatusNotFound, rec.Code, "somebody else's token")
}

func TestJoinTokenPatchStoresLikeCreation(t *testing.T) {
	f := newJoinTokenFixture(t)

	rec := testRequest(t, f.app.handlePatchTokens, testOwnerPrn, http.MethodPatch, "/devices/tokens/x",
		strings.NewReader(`{"nick":"line_two","default-user-meta":{"site.room":"b12"}}`), tokenParam(f.active))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"site.room":"b12"`)

	doc := f.stored(t, f.active)
	assert.Equal(t, "line_two", doc["nick"])
	assert.Equal(t, bson.M{"siteＮroom": "b12"}, doc["defaultusermeta"], "quoted, or Mongo would read the key as a path")
	assert.NotContains(t, doc, "time-modified")
	assert.True(t, doc["timemodified"].(primitive.DateTime).Time().After(f.created), "the field the model reads")

	rec = testRequest(t, f.app.handlePatchTokens, testOwnerPrn, http.MethodPatch, "/devices/tokens/x",
		strings.NewReader(`{"nick":"revived"}`), tokenParam(f.disabled))
	assert.Equal(t, http.StatusNotFound, rec.Code, "a disabled token cannot be changed")
	assert.Equal(t, "retired", f.stored(t, f.disabled)["nick"])
}
