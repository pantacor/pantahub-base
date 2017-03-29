//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
//
package auth

import (
	"strings"

	"pantahub-base/devices"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var passwords = map[string]string{
	"admin":    "admin",
	"user1":    "user1",
	"user2":    "user2",
	"service1": "service1",
	"service2": "service2",
	"service3": "service3",
	"device1":  "device1",
	"device2":  "device2",
}

var payloads = map[string]map[string]interface{}{
	"admin": map[string]interface{}{
		"roles": "admin",
		"type":  "USER",
		"prn":   "prn:pantahub.com:auth:/admin",
	},
	"user1": map[string]interface{}{
		"roles": "user",
		"type":  "USER",
		"prn":   "prn:pantahub.com:auth:/user1",
	},
	"user2": map[string]interface{}{
		"roles": "user",
		"type":  "USER",
		"prn":   "prn:pantahub.com:auth:/user2",
	},
	"service1": map[string]interface{}{
		"roles": "service",
		"type":  "SERVICE",
		"prn":   "prn:pantahub.com:auth:/service1",
	},
	"service2": map[string]interface{}{
		"roles": "service",
		"type":  "SERVICE",
		"prn":   "prn:pantahub.com:auth:/service2",
	},
	"service3": map[string]interface{}{
		"roles": "user",
		"type":  "SERVICE",
		"prn":   "prn:pantahub.com:auth:/service3",
	},
	"device1": map[string]interface{}{
		"roles": "device",
		"type":  "DEVICE",
		"prn":   "prn:pantahub.com:auth:/device1",
		"owner": "prn:pantahub.com:auth:/user1",
	},
	"device2": map[string]interface{}{
		"roles": "device",
		"type":  "DEVICE",
		"prn":   "prn:pantahub.com:auth:/device2",
		"owner": "prn:pantahub.com:auth:/user2",
	},
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

type AuthApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *AuthApp {

	app := new(AuthApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	jwtMiddleware.Authenticator = func(userId string, password string) bool {
		if passwords[userId] != "" && passwords[userId] == password {
			return true
		}
		if strings.HasPrefix(userId, "prn:::devices:") {
			return app.deviceAuth(userId, password)
		}
		return false
	}

	jwtMiddleware.PayloadFunc = func(userId string) map[string]interface{} {

		if plm, ok := payloads[userId]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				return *app.devicePayload(userId)
			} else {
				// XXX: FAIL HARD HERE! CODE SHOULD NEVER BE REACHABLE??
			}
		} else {
			return plm
		}
		return map[string]interface{}{}
	}

	app.Api = rest.NewApi()
	app.Api.Use(rest.DefaultDevStack...)
	app.Api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return request.URL.Path != "/login"
		},
		IfTrue: app.jwt_middleware,
	})

	// /login /auth_status and /refresh_token endpoints
	api_router, _ := rest.MakeRouter(
		rest.Post("/login", app.jwt_middleware.LoginHandler),
		rest.Get("/auth_status", handle_auth),
		rest.Get("/login", app.jwt_middleware.RefreshHandler),
	)
	app.Api.SetApp(api_router)

	return app
}

// XXX: make this a nice prn helper tool
func prnGetId(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1 : len(prn)]
}

func (a *AuthApp) deviceAuth(deviceId string, secret string) bool {

	c := a.mgoSession.DB("").C("pantahub_devices")

	id := prnGetId(deviceId)
	mgoId := bson.ObjectIdHex(id)

	device := devices.Device{}
	c.FindId(mgoId).One(&device)
	if secret == device.Secret {
		return true
	}
	return false
}

func (a *AuthApp) devicePayload(deviceId string) *map[string]interface{} {

	c := a.mgoSession.DB("").C("pantahub_devices")

	id := prnGetId(deviceId)
	mgoId := bson.ObjectIdHex(id)

	device := devices.Device{}
	err := c.FindId(mgoId).One(&device)

	if err != nil {
		return nil
	}

	val := map[string]interface{}{
		"roles": "device",
		"type":  "DEVICE",
		"prn":   device.Prn,
		"owner": device.Owner,
	}

	return &val
}
