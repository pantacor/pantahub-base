package healthz

import (
	"net/http"

	"log"
	"os"
	"path"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type HealthzApp struct {
	Api        *rest.Api
	mgoSession *mgo.Session
}

type Response struct {
	ErrorCode int           `json:"code"`
	Duration  time.Duration `json:"duration"`
	Start     time.Time     `json:"start-time"`
}

func (a *HealthzApp) handle_healthz(w rest.ResponseWriter, r *rest.Request) {

	response := Response{}

	response.Start = time.Now()

	user := r.Env["REMOTE_USER"].(string)

	if user == "" {
		rest.Error(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	// check DB
	collection := a.mgoSession.DB("").C("pantahub_devices")
	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	var val interface{}
	err := collection.Find(bson.M{}).One(val)
	if err != nil {
		rest.Error(w, "Error with Database query", http.StatusInternalServerError)
		return
	}
	// check storage
	s3Path := utils.GetEnv(utils.ENV_PANTAHUB_S3PATH)

	_, err = os.Stat(path.Join(s3Path, "HEALTHZ.txt"))

	if err != nil {
		rest.Error(w, "Error getting stats of HEALTHZ.txt on local-s3 storage", http.StatusInternalServerError)
		return
	}

	end := time.Now()
	response.Duration = end.Sub(response.Start)

	w.WriteJson(response)
}

func New(session *mgo.Session) *HealthzApp {

	app := new(HealthzApp)
	app.mgoSession = session

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{Logger: log.New(os.Stdout,
		"/health:", log.Lshortfile)})
	app.Api.Use(rest.DefaultCommonStack...)

	saAdminSecret := utils.GetEnv(utils.ENV_PANTAHUB_SA_ADMIN_SECRET)

	basicAuthMW := &rest.AuthBasicMiddleware{
		Realm: "Pantahub Health @ " + utils.GetEnv(utils.ENV_PANTAHUB_AUTH),
		Authenticator: func(userId string, password string) bool {
			return saAdminSecret != "" && userId == "saadmin" && password == saAdminSecret
		},
	}

	// no authentication needed for /login
	app.Api.Use(basicAuthMW)

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/", app.handle_healthz),
	)
	app.Api.SetApp(api_router)

	return app
}
