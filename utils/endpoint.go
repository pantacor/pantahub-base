package utils

import (
	"strings"
)

func GetApiEndpoint(localEndpoint string) string {

	// XXX: this is a hack for nginx proxying apparently not setting right scheme
	urlScheme := GetEnv(ENV_PANTAHUB_SCHEME)
	urlHost := GetEnv(ENV_PANTAHUB_HOST)
	urlPort := GetEnv(ENV_PANTAHUB_PORT)
	urlApiVersion := GetEnv(ENV_PANTAHUB_APIVERSION)

	url := urlScheme + "://" + urlHost

	if urlPort != "" {
		url += ":" + urlPort
	}

	if urlApiVersion != "" {
		url += "/" + urlApiVersion
	}

	if localEndpoint == "" {
		return url
	}

	if !strings.HasPrefix(localEndpoint, "/") {
		url += "/"
	}

	url += localEndpoint
	return url
}
