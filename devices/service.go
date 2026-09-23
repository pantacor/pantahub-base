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
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/caclient"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// PantahubDevicesAutoTokenV1 device auto token name
//
// #nosec G101 -- the name of an HTTP header, not a token value
const PantahubDevicesAutoTokenV1 = "Pantahub-Devices-Auto-Token-V1"
const CreateIndexTimeout = 600 * time.Second

// DeviceNickRule : Device nick rule used to create/update a device nick
const DeviceNickRule = `(?m)^[a-zA-Z0-9_\-+%]+$`

// App Web app structure
type App struct {
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
	subService  subscriptions.SubscriptionService
}

// Build factory a new Device App only with mongoClient
func Build(mongoClient *mongo.Client, subService subscriptions.SubscriptionService) *App {
	return &App{
		mongoClient: mongoClient,
		subService:  subService,
	}
}

// ModelError error type
type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Device device structure
type Device struct {
	ID        primitive.ObjectID `json:"id" bson:"_id"`
	Prn       string             `json:"prn"`
	Nick      string             `json:"nick"`
	Owner     string             `json:"owner"`
	OwnerNick string             `json:"owner-nick,omitempty" bson:"-"`
	// Secret is returned on registration only; SecretHash (sha256) is what is
	// stored at rest. Legacy rows still carry a plaintext "secret" field until
	// the background migration and PANTAHUB_PURGE_PLAINTEXT_SECRETS remove it.
	Secret              string                 `json:"secret,omitempty" bson:"-"`
	SecretHash          string                 `json:"-" bson:"secret_hash,omitempty"`
	TimeCreated         time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified        time.Time              `json:"time-modified" bson:"timemodified"`
	MetaModified        time.Time              `json:"meta-modified" bson:"meta-modified"`
	Challenge           string                 `json:"challenge,omitempty"`
	IsPublic            bool                   `json:"public" bson:"ispublic"`
	UserMeta            map[string]interface{} `json:"user-meta" bson:"user-meta"`
	DeviceMeta          map[string]interface{} `json:"device-meta" bson:"device-meta"`
	Garbage             bool                   `json:"garbage" bson:"garbage"`
	MarkPublicProcessed bool                   `json:"mark_public_processed" bson:"mark_public_processed"`

	OwnershipUnverify bool                    `json:"ownership_unverified" bson:"ownership_unverified"`
	OVMode            *models.OVModeExtension `json:"ovmode,omitempty" bson:"ovmode,omitempty"`
}

// PublicView is what anybody but the owner and the device itself sees of a
// public device. Fields are copied by name, so a field added to Device later
// stays private until it is added here.
func (d *Device) PublicView() Device {
	return Device{
		ID:           d.ID,
		Prn:          d.Prn,
		Nick:         d.Nick,
		Owner:        d.Owner,
		OwnerNick:    d.OwnerNick,
		IsPublic:     d.IsPublic,
		TimeCreated:  d.TimeCreated,
		TimeModified: d.TimeModified,
		UserMeta:     map[string]interface{}{},
		DeviceMeta:   map[string]interface{}{},
	}
}

type autoTokenInfo struct {
	TokenID  string
	Owner    string
	UserMeta map[string]interface{}
	OVMode   *models.OVModeExtension
}

// New create devices web app
func New(jwtConfig *jwtauth.Config, subService subscriptions.SubscriptionService, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtConfig = jwtConfig
	app.mongoClient = mongoClient
	app.subService = subService

	_, err := caclient.GetDefaultCAClient()
	if err != nil {
		if err, ok := err.(*caclient.ClientError); ok {
			if err.Code != caclient.ErrorNotConfig {
				log.Fatalf("Error loading caclient. Error Code: %d -- %s", err.Code, err.Error())
				return nil
			}
		}
	}

	err = app.EnsureDevicesIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub_devices: " + err.Error())
		return nil
	}

	err = app.EnsureTokenIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	// hash legacy plaintext device secrets in small throttled batches in the
	// background; devices that log in meanwhile upgrade themselves on the way
	go RunSecretMigration(context.Background(), mongoClient.Database(utils.MongoDb).Collection("pantahub_devices"))

	return app
}

// needsAuth mirrors the go-json-rest condition: bearer calls always
// authenticate; anonymous POST / and POST /register (device registration)
// pass through.
func needsAuth(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth)), "bearer ") {
		return true
	}
	return !((r.Method == "POST" && r.URL.Path == "/") ||
		(r.Method == "POST" && r.URL.Path == "/register"))
}

// Mount registers devices on echo with its previous middleware stack.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/devices"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/devices:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "devices"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{
				"Accept",
				"Content-Type",
				"Content-Length",
				"X-Custom-Header",
				"Origin",
				"Authorization",
				"X-Trace-ID",
				"Trace-Id",
				"x-request-id",
				"X-Request-ID",
				"TraceID",
				"ParentID",
				"Uber-Trace-ID",
				"uber-trace-id",
				"traceparent",
				"tracestate",
				"Ssl-Client-Verify",
				"Ssl-Client-Cert",
				"ssl-client-verify",
				"ssl-client-cert",
			},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.If(prefix, needsAuth, echoutil.JWT(app.jwtConfig)),
		echoutil.If(prefix, needsAuth, echoutil.Auth()),
	)

	writeDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.WriteDevices,
	}
	readDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	}
	updateDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.UpdateDevices,
	}
	validateDeviceScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.UpdateDevices,
		utils.Scopes.ValidateDevices,
	}

	// TPM auto enroll register
	g.POST("/register", app.handleRegister)

	// Device Ownership validation
	g.POST("/:id/ownership/validate", echoutil.ScopeFilter(validateDeviceScopes, app.handleValidateOwnership))

	// token api
	g.POST("/tokens", echoutil.ScopeFilter(updateDevicesScopes, app.handlePostTokens))
	g.DELETE("/tokens/:id", echoutil.ScopeFilter(updateDevicesScopes, app.handleDisableTokens))
	g.PATCH("/tokens/:id", echoutil.ScopeFilter(updateDevicesScopes, app.handlePatchTokens))
	g.GET("/tokens/:id", echoutil.ScopeFilter(readDevicesScopes, app.handleGetToken))
	g.GET("/tokens", echoutil.ScopeFilter(readDevicesScopes, app.handleGetTokens))

	// default api
	g.GET("/auth_status", echoutil.ScopeFilter(readDevicesScopes, handleAuth))
	g.GET("/", echoutil.ScopeFilter(readDevicesScopes, app.handleGetDevices))
	g.POST("/", echoutil.ScopeFilterOptionalAuth(writeDevicesScopes,
		func(c *echo.Context) error {
			userAgent := c.Request().Header.Get("User-Agent")
			if userAgent == "" {
				return echoutil.RestErrorWrapperUser(c, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
			}
			return app.handlePostDevice(c)
		}))
	g.GET("/:id", echoutil.ScopeFilter(readDevicesScopes, app.handleGetDevice))
	g.PUT("/:id", echoutil.ScopeFilter(writeDevicesScopes, app.handlePutDevice))
	g.PATCH("/:id", echoutil.ScopeFilter(writeDevicesScopes, app.handlePatchDevice))
	g.PUT("/:id/public", echoutil.ScopeFilter(writeDevicesScopes, app.handlePutPublic))
	g.DELETE("/:id/public", echoutil.ScopeFilter(writeDevicesScopes, app.handleDeletePublic))
	g.GET("/:id/user-meta", echoutil.ScopeFilter(readDevicesScopes, app.handleGetUserData))
	g.PUT("/:id/user-meta", echoutil.ScopeFilter(writeDevicesScopes, app.handlePutUserData))
	g.PATCH("/:id/user-meta", echoutil.ScopeFilter(writeDevicesScopes, app.handlePatchUserData))
	g.PUT("/:id/device-meta", echoutil.ScopeFilter(writeDevicesScopes, app.handlePutDeviceData))
	g.PATCH("/:id/device-meta", echoutil.ScopeFilter(writeDevicesScopes, app.handlePatchDeviceData))
	g.DELETE("/:id", echoutil.ScopeFilter(writeDevicesScopes, app.handleDeleteDevice))
	// lookup by nick-path (np)
	g.GET("/np/:usernick/:devicenick", echoutil.ScopeFilter(readDevicesScopes, app.handleGetUserDevice))
}
