// Copyright (c) 2026 Pantacor Ltd.
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

package exports

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// envTestMongo points these tests at a disposable MongoDB, for example
// mongodb://127.0.0.1:27017. They are skipped without it.
const envTestMongo = "PANTAHUB_EXPORTS_TEST_MONGO"

const uploadOwner = "prn:pantahub.com:auth:/user1"

// objectStore stands in for the /local-s3/ file server: it keeps what it is
// sent by sha256, and refuses everything when told to.
type objectStore struct {
	mu     sync.Mutex
	got    map[string][]byte
	puts   int
	refuse bool
}

func (o *objectStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, "/local-s3/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	o.puts++
	if o.refuse {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(body)
	o.got[hex.EncodeToString(sum[:])] = body

	// Like the real server, leave the content where storage looks for it.
	tok, err := objects.NewFromValidToken(path.Base(r.URL.Path))
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	claims, _ := tok.Claims.(*objects.ObjectAccessClaims)
	if claims == nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	storageID, _ := claims.StorageID()
	file, err := utils.MakeLocalS3PathForName(storageID)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(file), 0o700)
	}
	if err == nil {
		err = os.WriteFile(file, body, 0o600)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func newUploadFixture(t *testing.T) (*App, *objectStore) {
	t.Helper()
	uri := os.Getenv(envTestMongo)
	if uri == "" {
		t.Skip(envTestMongo + " is not set")
	}
	useTestKeys(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))

	previousDb := utils.MongoDb
	utils.MongoDb = fmt.Sprintf("exports_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		client.Database(utils.MongoDb).Drop(context.Background())
		utils.MongoDb = previousDb
		client.Disconnect(context.Background())
	})

	t.Setenv(utils.EnvPantahubStorageDriver, "local")
	t.Setenv(utils.EnvPantahubS3Path, t.TempDir())
	t.Setenv(utils.EnvPantahubStoragePath, "")

	store := &objectStore{got: map[string][]byte{}}
	app := Build(client)
	app.SetObjectServer(store)
	return app, store
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// export writes a pvr export: json first, then objects/<sha>.
func export(t *testing.T, state map[string]interface{}, objects ...[]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	if state != nil {
		stateBuf, err := json.Marshal(state)
		require.NoError(t, err)
		add("json", stateBuf)
	}
	for _, object := range objects {
		add("objects/"+sha(object), object)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func uploadToken(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	return parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
}

func put(t *testing.T, app *App, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, UploadPath+"/"+token, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "token", Value: token}})
	require.NoError(t, app.handlePutExportUpload(c))
	return rec
}

func TestAnUploadLinkTakesOneExport(t *testing.T) {
	app, store := newUploadFixture(t)
	ctx := context.Background()

	squashfs := []byte("the squashfs of an app")
	state := map[string]interface{}{
		"#spec":             "pantavisor-service-system@1",
		"web/root.squashfs": sha(squashfs),
		"web/run.json":      map[string]interface{}{"name": "web"},
	}

	upload, link, expires, err := app.CreateUpload(ctx, uploadOwner)
	require.NoError(t, err)
	assert.Equal(t, UploadWaiting, upload.Status)
	assert.WithinDuration(t, time.Now().Add(uploadLinkTTL), expires, 5*time.Second)
	assert.True(t, strings.HasPrefix(link, utils.GetAPIEndpoint(UploadPath+"/")), link)

	rec := put(t, app, uploadToken(t, link), export(t, state, squashfs))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got, err := app.GetUpload(ctx, uploadOwner, upload.ID)
	require.NoError(t, err)
	assert.Equal(t, UploadReceived, got.Status)
	assert.Equal(t, []string{sha(squashfs)}, got.Objects)
	assert.Equal(t, 1, got.Stored)
	assert.Empty(t, got.Missing)
	assert.Equal(t, map[string]interface{}{"name": "web"}, got.State["web/run.json"])
	assert.Equal(t, squashfs, store.got[sha(squashfs)], "stored through the object server")

	// The link is spent.
	rec = put(t, app, uploadToken(t, link), export(t, state, squashfs))
	assert.Equal(t, http.StatusConflict, rec.Code)

	// Nobody else sees it.
	_, err = app.GetUpload(ctx, "prn:pantahub.com:auth:/user2", upload.ID)
	assert.ErrorIs(t, err, ErrUploadNotFound)
}

func TestObjectsTheAccountHasAreNotUploadedAgain(t *testing.T) {
	app, store := newUploadFixture(t)
	ctx := context.Background()
	squashfs := []byte("shared between two exports")
	state := map[string]interface{}{"#spec": "pantavisor-service-system@1", "web/root.squashfs": sha(squashfs)}

	for i := 0; i < 2; i++ {
		_, link, _, err := app.CreateUpload(ctx, uploadOwner)
		require.NoError(t, err)
		rec := put(t, app, uploadToken(t, link), export(t, state, squashfs))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	assert.Equal(t, 1, store.puts, "the second export found the object already stored")
}

func TestBrokenExportsFailWithAReason(t *testing.T) {
	app, store := newUploadFixture(t)
	ctx := context.Background()
	squashfs := []byte("content")

	cases := map[string]struct {
		body   []byte
		refuse bool
		reason string
	}{
		// Each case its own content: an object stored before a later failure
		// stays in the account, as with a partial pvr post.
		"no state":            {export(t, nil, append(squashfs, 1)), false, "no json state"},
		"not an archive":      {[]byte("hello, this is no tar"), false, "not a valid tar"},
		"content not its sha": {export(t, map[string]interface{}{"#spec": "pantavisor-service-system@1"}, append(squashfs, 2)), true, "does not match"},
	}
	for name, tc := range cases {
		store.refuse = tc.refuse
		upload, link, _, err := app.CreateUpload(ctx, uploadOwner)
		require.NoError(t, err)
		rec := put(t, app, uploadToken(t, link), tc.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)

		got, err := app.GetUpload(ctx, uploadOwner, upload.ID)
		require.NoError(t, err)
		assert.Equal(t, UploadFailed, got.Status, name)
		assert.Contains(t, got.Error, tc.reason, name)
	}
	store.refuse = false

	// An object named by something that is no sha256.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "objects/not-a-sha", Size: 1, Mode: 0644, Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte("x"))
	require.NoError(t, tw.Close())
	upload, link, _, err := app.CreateUpload(ctx, uploadOwner)
	require.NoError(t, err)
	put(t, app, uploadToken(t, link), buf.Bytes())
	got, err := app.GetUpload(ctx, uploadOwner, upload.ID)
	require.NoError(t, err)
	assert.Contains(t, got.Error, "not named by a sha256")
}

func TestAnExportNamingObjectsNobodyHasSaysSo(t *testing.T) {
	app, _ := newUploadFixture(t)
	ctx := context.Background()
	state := map[string]interface{}{
		"#spec":             "pantavisor-service-system@1",
		"web/root.squashfs": sha([]byte("never uploaded anywhere")),
	}
	upload, link, _, err := app.CreateUpload(ctx, uploadOwner)
	require.NoError(t, err)
	rec := put(t, app, uploadToken(t, link), export(t, state))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got, err := app.GetUpload(ctx, uploadOwner, upload.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"web/root.squashfs"}, got.Missing)
}

func TestUploadLinksCannotBeForged(t *testing.T) {
	app, _ := newUploadFixture(t)
	key := useTestKeys(t)

	upload, link, _, err := app.CreateUpload(context.Background(), uploadOwner)
	require.NoError(t, err)

	// A download link, or one made for another account's upload, is refused.
	download, _, err := SignDownloadLink(LinkRequest{OwnerPrn: uploadOwner, OwnerNick: "user1", DeviceNick: "d", Rev: 0}, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, put(t, app, tokenOf(t, download), []byte("x")).Code)

	stolen, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud": uploadAudience(), "sub": "prn:pantahub.com:auth:/user2", claimUploadID: upload.ID,
		"exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString(key)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, put(t, app, stolen, []byte("x")).Code, "the upload is not theirs to take")

	expired, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud": uploadAudience(), "sub": uploadOwner, claimUploadID: upload.ID,
		"exp": time.Now().Add(-time.Minute).Unix(),
	}).SignedString(key)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, put(t, app, expired, []byte("x")).Code)

	claims, err := parseUploadToken(uploadToken(t, link))
	require.NoError(t, err)
	assert.True(t, utils.IsResourceBoundAudience(claims["aud"]), "no login anywhere")
}

func TestImportsAreFetchedOnlyFromAllowedHosts(t *testing.T) {
	app, _ := newUploadFixture(t)
	ctx := context.Background()

	t.Setenv(EnvImportAllowedHosts, "")
	_, err := app.ImportFromURL(ctx, uploadOwner, "https://gitlab.com/x/export.tar.gz")
	assert.ErrorIs(t, err, ErrImportDisabled)

	t.Setenv(EnvImportAllowedHosts, "gitlab.com")
	for name, raw := range map[string]string{
		"cleartext":      "http://gitlab.com/x/export.tar.gz",
		"another host":   "https://evil.example.com/export.tar.gz",
		"credentials":    "https://user:pass@gitlab.com/export.tar.gz",
		"not even a URL": "://",
	} {
		_, err := app.ImportFromURL(ctx, uploadOwner, raw)
		assert.Error(t, err, name)
	}
}
