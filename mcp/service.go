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

// Package mcp serves a Model Context Protocol endpoint so an AI assistant
// (claude.ai, Claude Code, MCP Inspector, ...) can manage the devices of the
// user who authorized it.
//
// The endpoint is a plain net/http handler mounted on the API's mux, like the
// MQTT WebSocket plane, and deliberately not a set of echo routes: the MCP SDK
// owns the whole request/response cycle of its single endpoint, and the tokens
// accepted here are audience-bound to it, unlike the ones the REST middleware
// takes. Authentication, scope enforcement and CORS are therefore done here,
// against the same RS256 keys and the same scope catalogue the REST API uses.
package mcp

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/rs/cors"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// EnvMcpEnabled turns the MCP endpoint on. It is off unless asked for.
	EnvMcpEnabled = "PANTAHUB_MCP_ENABLED"

	// EnvMcpPath is the path the endpoint is mounted under.
	EnvMcpPath = "PANTAHUB_MCP_PATH"

	// EnvMcpResourceURL overrides the public URL of the endpoint, which is
	// otherwise derived from PANTAHUB_SCHEME, PANTAHUB_HOST and PANTAHUB_PORT.
	// It is the OAuth resource identifier (RFC 8707): tokens are bound to it
	// and the protected resource metadata advertises it, so it has to be the
	// exact URL a user enters in the MCP client. Set it when the API is reached
	// through a tunnel or a proxy that the PANTAHUB_* values do not describe.
	EnvMcpResourceURL = "PANTAHUB_MCP_RESOURCE_URL"

	// EnvMcpAllowAPITokens accepts ordinary API login tokens, which carry no
	// audience, on the MCP endpoint. Off by default: a token presented here is
	// expected to have been issued for this resource. Meant for development,
	// to exercise the tools before a client can obtain an audience-bound token.
	//#nosec G101 -- the name of an environment variable, not a token value
	EnvMcpAllowAPITokens = "PANTAHUB_MCP_ALLOW_API_TOKENS"

	defaultPath = "/mcp"

	// prmWellKnown is where RFC 9728 puts protected resource metadata. Clients
	// look for it under the resource's own path first and at the root second.
	prmWellKnown = "/.well-known/oauth-protected-resource"

	// maxRequestBody bounds a JSON-RPC request. Tool arguments here are a few
	// short strings; nothing legitimate comes close.
	maxRequestBody = 1 << 20

	serverName    = "pantahub"
	serverVersion = "0.1.0"
)

// Enabled reports whether the MCP endpoint should be served.
func Enabled() bool {
	return envBool(EnvMcpEnabled)
}

func envBool(key string) bool {
	enabled, err := strconv.ParseBool(utils.GetEnvDefault(key, "false"))
	if err != nil {
		return false
	}
	return enabled
}

// Path is the path the endpoint is mounted under, without a trailing slash: MCP
// is a single endpoint, not a subtree.
func Path() string {
	path := strings.TrimRight(utils.GetEnvDefault(EnvMcpPath, defaultPath), "/")
	if path == "" {
		return defaultPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// Service is the MCP endpoint together with its OAuth resource metadata.
type Service struct {
	store       *store
	connections *connectionChecker

	resourceURL    string
	metadataURL    string
	allowAPITokens bool

	handler  http.Handler
	metadata http.Handler

	// uploads takes pvr exports into accounts; see SetExportUploads.
	uploads exportUploads
}

// New builds the MCP service. logsApp must be the App built by logs.New; see
// logs.App.GetLogs.
func New(mongoClient *mongo.Client, logsApp *logs.App) (*Service, error) {
	if mongoClient == nil {
		return nil, errors.New("mcp: no mongo client")
	}
	if logsApp == nil {
		return nil, errors.New("mcp: no logs app")
	}

	resourceURL, err := canonicalResourceURL(utils.GetEnvDefault(EnvMcpResourceURL, utils.GetAPIEndpoint(Path())))
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(resourceURL)
	origin := parsed.Scheme + "://" + parsed.Host

	s := &Service{
		store:          &store{mongoClient: mongoClient, logsApp: logsApp},
		connections:    newConnectionChecker(),
		resourceURL:    resourceURL,
		metadataURL:    origin + prmWellKnown + parsed.Path,
		allowAPITokens: envBool(EnvMcpAllowAPITokens),
	}

	server := sdk.NewServer(&sdk.Implementation{
		Name:    serverName,
		Title:   "Pantahub",
		Version: serverVersion,
	}, &sdk.ServerOptions{
		Instructions: serverInstructions + "\n\n" + revisionInstructions,
	})
	s.registerTools(server)
	s.registerRevisionTools(server)
	s.registerUploadTools(server)

	// Stateless with plain JSON responses: every tool answers in one round
	// trip and nothing is ever pushed to the client, so there is no session to
	// keep and no stream to hold open. It also means any replica can serve any
	// request.
	streamable := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	bearer := auth.RequireBearerToken(s.verifyToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: s.metadataURL,
	})

	s.handler = corsPolicy().Handler(
		http.MaxBytesHandler(challengeWithDefaultScopes(bearer(s.requireToolScopes(streamable))), maxRequestBody),
	)

	s.metadata = auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource: s.resourceURL,
		// The API is its own authorization server; its issuer is the API root.
		AuthorizationServers:   []string{utils.GetAPIEndpoint("")},
		ScopesSupported:        supportedScopes(),
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Pantahub device management",
	})

	return s, nil
}

// Handler serves the MCP endpoint. Mount it at Path().
func (s *Service) Handler() http.Handler {
	return s.handler
}

// MetadataHandler serves the RFC 9728 protected resource metadata. Mount it at
// every path MetadataPaths returns.
func (s *Service) MetadataHandler() http.Handler {
	return s.metadata
}

// MetadataPaths lists where clients look for the protected resource metadata:
// the path-qualified location the 401 challenge points at, and the root one
// some clients fall back to.
func (s *Service) MetadataPaths() []string {
	return []string{prmWellKnown + Path(), prmWellKnown}
}

// ResourceURL is the canonical public URL of the endpoint, the audience a token
// must carry.
func (s *Service) ResourceURL() string {
	return s.resourceURL
}

// Scopes are the scopes a token for this endpoint may carry. The authorization
// server is told about them so it never binds anything else to this audience.
func (s *Service) Scopes() []string {
	return supportedScopes()
}

// DefaultScopes are what a token gets when the client names none: read access.
func (s *Service) DefaultScopes() []string {
	return defaultScopes()
}

// challengeWithDefaultScopes adds scope="..." to the 401 challenge the bearer
// middleware writes. A client takes the scopes to request from there before it
// looks at the metadata, which lists every scope: without this a first
// connection would ask the user for write access it may never need.
func challengeWithDefaultScopes(next http.Handler) http.Handler {
	scope := strings.Join(defaultScopes(), " ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&challengeWriter{ResponseWriter: w, scope: scope}, r)
	})
}

type challengeWriter struct {
	http.ResponseWriter
	scope string
	wrote bool
}

func (c *challengeWriter) WriteHeader(status int) {
	if !c.wrote && status == http.StatusUnauthorized {
		if challenge := c.Header().Get("WWW-Authenticate"); challenge != "" && !strings.Contains(challenge, "scope=") {
			c.Header().Set("WWW-Authenticate", challenge+`, scope="`+c.scope+`"`)
		}
	}
	c.wrote = true
	c.ResponseWriter.WriteHeader(status)
}

func (c *challengeWriter) Write(body []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(body)
}

// Flush keeps streaming responses working through the wrapper.
func (c *challengeWriter) Flush() {
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (c *challengeWriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

// canonicalResourceURL spells the endpoint URL the way the authorization server
// spells the audience it binds tokens to.
func canonicalResourceURL(raw string) (string, error) {
	canonical, err := utils.CanonicalResourceURL(raw)
	if err != nil {
		return "", errors.New("mcp: " + err.Error())
	}
	return canonical, nil
}

// corsPolicy lets browser based MCP clients call the endpoint. Any origin may:
// the endpoint authenticates with a bearer header and never with cookies, so
// there is no ambient credential for a hostile page to ride on. The MCP headers
// have to be listed because none of them is CORS-safelisted.
func corsPolicy() *cors.Cors {
	return cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{http.MethodPost, http.MethodGet, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{
			"Authorization", "Content-Type", "Accept",
			"Mcp-Protocol-Version", "Mcp-Session-Id", "Mcp-Method", "Mcp-Name", "Last-Event-ID",
		},
		ExposedHeaders: []string{"Mcp-Session-Id", "WWW-Authenticate"},
		MaxAge:         600,
	})
}
