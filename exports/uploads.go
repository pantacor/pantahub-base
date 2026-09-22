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
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/safehttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// An export upload brings a pvr export (the .tar.gz `pvr export` writes: a
// json state and objects/<sha256> files) into an account, the way pvtx in the
// web app takes one: every object is stored through the same path a client
// upload takes, and the state is kept for a revision plan to merge in. It
// arrives either through a single-use upload link, or fetched by the server
// from a URL on a configured allow list.

const (
	// UploadPath is where upload links are served, under the API.
	UploadPath = "/exports/uploads"

	// EnvUploadMaxBytes bounds one export archive, compressed. Default 2 GiB.
	EnvUploadMaxBytes = "PANTAHUB_EXPORT_UPLOAD_MAX_BYTES"

	// EnvImportAllowedHosts lists, comma separated, the hosts exports may be
	// fetched from, such as "gitlab.com,storage.googleapis.com" for CI
	// artifacts and the storage they redirect to. Empty turns fetching off.
	EnvImportAllowedHosts = "PANTAHUB_EXPORT_IMPORT_ALLOWED_HOSTS"

	uploadsCollection  = "pantahub_export_uploads"
	uploadTTL          = time.Hour
	uploadLinkTTL      = 30 * time.Minute
	defaultMaxUpload   = 2 << 30
	maxStateJSON       = 32 << 20
	importFetchTimeout = 30 * time.Minute
	maxImportRedirects = 3

	claimUploadID = "upl"

	// UploadWaiting and the other statuses tell how far an upload got.
	UploadWaiting   = "waiting"
	UploadReceiving = "receiving"
	UploadReceived  = "received"
	UploadFailed    = "failed"
)

var (
	// ErrUploadNotFound covers an upload that does not exist, expired, or is
	// somebody else's.
	ErrUploadNotFound = errors.New("no such export upload")

	// ErrImportDisabled means no host is allowed to fetch exports from.
	ErrImportDisabled = errors.New("fetching exports from a URL is not enabled on this server")
)

// Upload is one export on its way into an account.
type Upload struct {
	ID     string `json:"id" bson:"_id"`
	Owner  string `json:"-" bson:"owner"`
	Status string `json:"status" bson:"status"`
	Source string `json:"source,omitempty" bson:"source,omitempty"`
	// State is the export's state document, merged into a revision by a plan.
	State map[string]interface{} `json:"-" bson:"state,omitempty"`
	// Objects are the objects the archive carried; Stored of them were new to
	// the account, the rest it already had.
	Objects []string `json:"objects,omitempty" bson:"objects,omitempty"`
	Stored  int      `json:"stored" bson:"stored"`
	// Missing are objects the state names that neither the archive nor the
	// account has: a revision using them would be refused.
	Missing   []string  `json:"missing,omitempty" bson:"missing,omitempty"`
	Size      int64     `json:"size" bson:"size"`
	Error     string    `json:"error,omitempty" bson:"error,omitempty"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time `json:"updated_at" bson:"updated_at"`
	ExpiresAt time.Time `json:"expires_at" bson:"expires_at"`
}

// SetObjectServer hands the importer the handler that stores object content,
// the one mounted at /local-s3/: objects of an export are stored through it
// exactly as a client upload is, with its size and sha256 checks.
func (app *App) SetObjectServer(handler http.Handler) {
	app.objectServer = handler
}

func (app *App) uploads() *mongo.Collection {
	return app.mongoClient.Database(utils.MongoDb).Collection(uploadsCollection)
}

func (app *App) ensureUploadIndexes(ctx context.Context) {
	_, err := app.uploads().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
	if err != nil {
		log.Println("WARNING: export uploads expiry index: " + err.Error())
	}
}

func newUploadID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (app *App) newUpload(ctx context.Context, owner, status, source string) (*Upload, error) {
	if owner == "" {
		return nil, errors.New("an upload belongs to an account")
	}
	app.ensureUploadIndexes(ctx)

	id, err := newUploadID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	upload := &Upload{
		ID: id, Owner: owner, Status: status, Source: source,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(uploadTTL),
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := app.uploads().InsertOne(ctx, upload); err != nil {
		return nil, err
	}
	return upload, nil
}

func uploadAudience() string {
	return utils.GetAPIEndpoint(UploadPath)
}

// CreateUpload starts an upload for owner and returns the single-use URL to
// PUT the archive to, and until when it takes one.
func (app *App) CreateUpload(ctx context.Context, owner string) (*Upload, string, time.Time, error) {
	upload, err := app.newUpload(ctx, owner, UploadWaiting, "upload")
	if err != nil {
		return nil, "", time.Time{}, err
	}
	keys, err := linkKeys()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	expires := time.Now().Add(uploadLinkTTL)
	// Bound to a URL audience, like download links, so it is no login.
	token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud":         uploadAudience(),
		"iss":         utils.GetAPIEndpoint(""),
		"sub":         owner,
		"exp":         expires.Unix(),
		"iat":         time.Now().Unix(),
		claimUploadID: upload.ID,
	}).SignedString(keys.PrivateKey)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return upload, utils.GetAPIEndpoint(UploadPath + "/" + token), expires, nil
}

// GetUpload returns one of owner's uploads.
func (app *App) GetUpload(ctx context.Context, owner, id string) (*Upload, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	upload := &Upload{}
	err := app.uploads().FindOne(ctx, bson.M{"_id": id, "owner": owner, "expires_at": bson.M{"$gt": time.Now()}}).Decode(upload)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, err
	}
	upload.State = utils.BsonUnquoteMap(&upload.State)
	return upload, nil
}

// ImportFromURL starts fetching an export from rawURL for owner. The fetch
// runs in the background; GetUpload tells how it went.
func (app *App) ImportFromURL(ctx context.Context, owner, rawURL string) (*Upload, error) {
	allowed := importAllowedHosts()
	if len(allowed) == 0 {
		return nil, ErrImportDisabled
	}
	target, err := checkImportURL(rawURL, allowed)
	if err != nil {
		return nil, err
	}

	upload, err := app.newUpload(ctx, owner, UploadReceiving, target.Hostname())
	if err != nil {
		return nil, err
	}

	// The fetch outlives the request that started it.
	detached := context.WithoutCancel(ctx)
	go func() {
		fetchCtx, cancel := context.WithTimeout(detached, importFetchTimeout)
		defer cancel()
		app.fetchAndReceive(fetchCtx, owner, upload.ID, target, allowed)
	}()
	return upload, nil
}

func importAllowedHosts() map[string]bool {
	hosts := map[string]bool{}
	for _, entry := range strings.Split(utils.GetEnvDefault(EnvImportAllowedHosts, ""), ",") {
		if entry = strings.ToLower(strings.TrimSpace(entry)); entry != "" {
			hosts[entry] = true
		}
	}
	return hosts
}

func checkImportURL(rawURL string, allowed map[string]bool) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	switch {
	case err != nil:
		return nil, fmt.Errorf("the URL is malformed: %w", err)
	case target.Scheme != "https":
		return nil, errors.New("exports are only fetched over https")
	case target.User != nil:
		return nil, errors.New("the URL must not carry credentials")
	case !allowed[strings.ToLower(target.Hostname())]:
		return nil, fmt.Errorf("%s is not a host exports may be fetched from on this server", target.Hostname())
	}
	return target, nil
}

func (app *App) fetchAndReceive(ctx context.Context, owner, id string, target *url.URL, allowed map[string]bool) {
	client := safehttp.NewClient(safehttp.Options{
		Timeout: importFetchTimeout,
		// CI artifacts redirect to where they are stored; that host has to be
		// allowed too.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxImportRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || !allowed[strings.ToLower(req.URL.Hostname())] {
				return fmt.Errorf("redirected to %s, which is not a host exports may be fetched from", req.URL.Hostname())
			}
			return nil
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		app.failUpload(owner, id, "the URL cannot be fetched")
		return
	}
	req.Header.Set("User-Agent", "pantahub-export-import/1")
	resp, err := client.Do(req) //#nosec G107,G704 -- https URL on an operator allow list, public addresses only (safehttp)
	if err != nil {
		app.failUpload(owner, id, "the export could not be fetched: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		app.failUpload(owner, id, fmt.Sprintf("fetching the export answered %d", resp.StatusCode))
		return
	}
	app.receive(ctx, owner, id, resp.Body)
}

func (app *App) failUpload(owner, id, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = app.uploads().UpdateOne(ctx,
		bson.M{"_id": id, "owner": owner},
		bson.M{"$set": bson.M{"status": UploadFailed, "error": reason, "updated_at": time.Now()}})
}

// handlePutExportUpload receives the archive an upload link was made for.
// @Summary Upload a pvr export through a signed link
// @Description Receives a pvr export (.tar.gz or .tar) for the upload the link was made for. Each link takes one archive.
// @Accept  application/gzip
// @Produce  json
// @Tags exports
// @Param token path string true "Upload token"
// @Success 200 {object} Upload
// @Failure 400 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Router /exports/uploads/{token} [put]
func (app *App) handlePutExportUpload(c *echo.Context) error {
	claims, err := parseUploadToken(c.Param("token"))
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "This upload link is not valid or has expired", http.StatusForbidden)
	}
	owner, _ := claims["sub"].(string)
	id, _ := claims[claimUploadID].(string)

	// One archive per link: the first request takes the upload.
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	result, err := app.uploads().UpdateOne(ctx,
		bson.M{"_id": id, "owner": owner, "status": UploadWaiting},
		bson.M{"$set": bson.M{"status": UploadReceiving, "updated_at": time.Now()}})
	if err != nil {
		return echoutil.RestErrorWrapper(c, "error starting the upload: "+err.Error(), http.StatusInternalServerError)
	}
	if result.MatchedCount == 0 {
		return echoutil.RestErrorWrapperUser(c, "upload already used or gone", "This upload link was already used", http.StatusConflict)
	}

	upload := app.receive(c.Request().Context(), owner, id, c.Request().Body)
	if upload == nil {
		return echoutil.RestErrorWrapper(c, "error reading the upload back", http.StatusInternalServerError)
	}
	if upload.Status != UploadReceived {
		return echoutil.RestErrorWrapperUser(c, upload.Error, upload.Error, http.StatusBadRequest)
	}
	return echoutil.WriteJSON(c, http.StatusOK, upload)
}

func parseUploadToken(raw string) (jwtgo.MapClaims, error) {
	keys, err := linkKeys()
	if err != nil {
		return nil, err
	}
	token, err := jwtgo.Parse(raw, func(t *jwtgo.Token) (interface{}, error) {
		if t.Method != jwtgo.SigningMethodRS256 {
			return nil, errors.New("unexpected signing method")
		}
		return keys.PublicKey, nil
	}, jwtgo.WithAudience(uploadAudience()), jwtgo.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired upload link")
	}
	claims, ok := token.Claims.(jwtgo.MapClaims)
	if !ok {
		return nil, errors.New("unreadable upload link")
	}
	if owner, _ := claims["sub"].(string); owner == "" {
		return nil, errors.New("upload link names no account")
	}
	if id, _ := claims[claimUploadID].(string); id == "" {
		return nil, errors.New("upload link names no upload")
	}
	return claims, nil
}

func maxUploadBytes() int64 {
	if n, err := strconv.ParseInt(utils.GetEnvDefault(EnvUploadMaxBytes, ""), 10, 64); err == nil && n > 0 {
		return n
	}
	return defaultMaxUpload
}

// countingReader counts what was read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// receive reads an archive into the upload id of owner, and records the
// outcome on it: received with its state, or failed with why.
func (app *App) receive(ctx context.Context, owner, id string, body io.Reader) *Upload {
	limit := maxUploadBytes()
	counted := &countingReader{r: io.LimitReader(body, limit+1)}

	set := bson.M{"updated_at": time.Now()}
	state, archived, stored, err := app.readArchive(ctx, owner, counted)
	if err == nil && counted.n > limit {
		err = fmt.Errorf("the export is larger than %d bytes", limit)
	}
	if err != nil {
		set["status"] = UploadFailed
		set["error"] = err.Error()
	} else {
		set["status"] = UploadReceived
		set["state"] = utils.BsonQuoteMap(&state)
		set["objects"] = archived
		set["stored"] = stored
		set["missing"] = app.missingObjects(ctx, owner, state, archived)
	}
	set["size"] = counted.n

	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	upload := &Upload{}
	err = app.uploads().FindOneAndUpdate(updateCtx,
		bson.M{"_id": id, "owner": owner},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(upload)
	if err != nil {
		log.Println("ERROR: recording export upload " + id + ": " + err.Error())
		return nil
	}
	upload.State = utils.BsonUnquoteMap(&upload.State)
	return upload
}

// readArchive reads a pvr export, gzipped or not, storing each object it
// carries for owner. It answers the state, every object sha it saw, and how
// many of those the account did not have before.
func (app *App) readArchive(ctx context.Context, owner string, r io.Reader) (map[string]interface{}, []string, int, error) {
	buffered := bufio.NewReader(r)
	var source io.Reader = buffered
	if magic, err := buffered.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, nil, 0, errors.New("the export is not a valid gzip archive")
		}
		defer gz.Close()
		source = gz
	}

	var state map[string]interface{}
	archived := []string{}
	stored := 0
	tr := tar.NewReader(source)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, 0, errors.New("the export is not a valid tar archive")
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")

		switch {
		case name == "json":
			if header.Size > maxStateJSON {
				return nil, nil, 0, errors.New("the export's state document is too large")
			}
			buf, err := io.ReadAll(io.LimitReader(tr, maxStateJSON))
			if err != nil {
				return nil, nil, 0, errors.New("the export's state document cannot be read")
			}
			// Decoded as the REST API decodes a posted state.
			if err := json.Unmarshal(buf, &state); err != nil {
				return nil, nil, 0, errors.New("the export's state document is not JSON")
			}

		case strings.HasPrefix(name, "objects/"):
			sha := strings.TrimPrefix(name, "objects/")
			if _, err := utils.DecodeSha256HexString(sha); err != nil {
				return nil, nil, 0, fmt.Errorf("%s is not named by a sha256", name)
			}
			isNew, err := app.storeObject(ctx, owner, sha, header.Size, tr)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("object %s: %w", sha, err)
			}
			archived = append(archived, sha)
			if isNew {
				stored++
			}
		}
	}

	if state == nil {
		return nil, nil, 0, errors.New("the archive has no json state; is it the output of pvr export?")
	}
	sort.Strings(archived)
	return state, archived, stored, nil
}

// storeObject stores one object of an export for owner through
// objects.CreateObject and the object file server, as POST /objects and the
// upload to its signed URL do. It reports false for an object the account
// already had, which it does not upload again.
func (app *App) storeObject(ctx context.Context, owner, sha string, size int64, content io.Reader) (bool, error) {
	if app.objectServer == nil {
		return false, errors.New("object storage is not available to imports")
	}
	objectsApp := objects.Build(app.mongoClient)

	object, err := objects.NewObject(sha, owner, sha)
	if err != nil {
		return false, err
	}
	object.Size = strconv.FormatInt(size, 10)
	object.SizeInt = size
	object.MimeType = "application/octet-stream"

	created, status, _, err := objectsApp.CreateObject(ctx, owner, *object, true)
	if err != nil {
		if utils.IsUserError(err) {
			return false, err
		}
		log.Println("ERROR: export import storing " + sha + ": " + err.Error())
		return false, errors.New("the object could not be stored")
	}
	if status == http.StatusConflict {
		return false, nil // the account has it, or links to a public copy
	}

	putURL, err := url.Parse(objects.GetObjectWithAccess(created, "/objects").SignedPutURL)
	if err != nil {
		return false, errors.New("the object could not be stored")
	}
	// The signed URL names the file server under whatever base the storage
	// URL has; the handler is mounted at /local-s3/.
	at := strings.Index(putURL.Path, "/local-s3/")
	if at < 0 {
		return false, errors.New("the object could not be stored")
	}
	target := putURL.Path[at:]
	if putURL.RawQuery != "" {
		target += "?" + putURL.RawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, content)
	if err != nil {
		return false, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")

	rec := httptest.NewRecorder()
	app.objectServer.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		log.Printf("ERROR: export import uploading %s: %d %s", sha, rec.Code, strings.TrimSpace(rec.Body.String()))
		if rec.Code == http.StatusBadRequest {
			return false, errors.New("its content does not match its sha256 or its size")
		}
		return false, errors.New("the object could not be stored")
	}
	return true, nil
}

// missingObjects lists the objects state names that neither the archive nor
// the account has.
func (app *App) missingObjects(ctx context.Context, owner string, state map[string]interface{}, archived []string) []string {
	have := map[string]bool{}
	for _, sha := range archived {
		have[sha] = true
	}
	objectsApp := objects.Build(app.mongoClient)
	missing := []string{}
	for key, value := range state {
		sha, ok := value.(string)
		if !ok || key == "#spec" || strings.HasSuffix(key, ".json") || have[sha] {
			continue
		}
		if _, err := objectsApp.ResolveObjectWithLinks(ctx, owner, sha, true); err != nil {
			missing = append(missing, key)
		}
		have[sha] = true
	}
	sort.Strings(missing)
	return missing
}
