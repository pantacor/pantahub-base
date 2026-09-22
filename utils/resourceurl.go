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

package utils

import (
	"errors"
	"net/url"
	"strings"
)

// CanonicalResourceURL normalizes the URL of an OAuth protected resource the
// way clients do before sending it as the RFC 8707 resource parameter:
// lowercase scheme and host, no default port, no trailing slash, no query or
// fragment. The authorization server that binds a token to a resource and the
// resource that checks the binding both compare in this form, so a harmless
// spelling difference cannot lock a client out.
func CanonicalResourceURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("invalid resource url: " + err.Error())
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("resource url needs a scheme and a host: " + raw)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", errors.New("resource url must be http or https: " + raw)
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}

	return scheme + "://" + host + strings.TrimRight(parsed.EscapedPath(), "/"), nil
}

// TokenAudiences reads an aud claim, which JWT allows to be one string or a
// list of them.
func TokenAudiences(claim interface{}) []string {
	switch aud := claim.(type) {
	case string:
		if aud == "" {
			return nil
		}
		return []string{aud}
	case []string:
		return aud
	case []interface{}:
		audiences := make([]string, 0, len(aud))
		for _, item := range aud {
			if value, ok := item.(string); ok && value != "" {
				audiences = append(audiences, value)
			}
		}
		return audiences
	}
	return nil
}

// IsResourceBoundAudience reports whether an aud claim binds the token to an
// OAuth protected resource, which is named by URL. Such a token is only good at
// that resource: every other token consumer must refuse it, or a token a user
// granted to one integration would quietly work everywhere (RFC 8707 section 2,
// and the "token passthrough" the MCP specification forbids).
//
// The audiences this API used before are PRNs — the service a token was issued
// on behalf of — and are unaffected.
func IsResourceBoundAudience(claim interface{}) bool {
	for _, audience := range TokenAudiences(claim) {
		lowered := strings.ToLower(audience)
		if strings.HasPrefix(lowered, "https://") || strings.HasPrefix(lowered, "http://") {
			return true
		}
	}
	return false
}
