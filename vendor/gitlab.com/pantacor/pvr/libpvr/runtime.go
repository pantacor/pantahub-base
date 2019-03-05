package libpvr

import (
	"github.com/urfave/cli"
)

var (
	RuntimeDefaultMetadata = map[string]interface{}{
		"PVR_BASEURL":      "https://api.pantahub.com",
		"PVR_REPO_BASEURL": "https://pvr.pantahub.com",
		"PVR_AUTH":         "",
	}
)

func DefaultSession() *Session {

	app := cli.NewApp()
	authConfig := newDefaultAuthConfig("")

	app.Metadata = RuntimeDefaultMetadata
	session := Session{app: app, auth: authConfig}
	return &session
}
