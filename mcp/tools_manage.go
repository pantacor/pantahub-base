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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// The tools in this file manage what already exists: a device's configuration,
// device join tokens and OAuth applications. None of them creates or deletes
// anything, and none of them can produce a secret. Creating a join token or a
// confidential application yields a secret that is shown exactly once, and
// through this endpoint it would be shown to an assistant and kept in its
// conversation; that stays in the web app, by decision.

const (
	toolUpdateUserMeta    = "update_user_meta"
	toolListDeviceTokens  = "list_device_tokens"
	toolGetDeviceToken    = "get_device_token"
	toolUpdateDeviceToken = "update_device_token"
	toolListApps          = "list_apps"
	toolGetApp            = "get_app"
	toolUpdateApp         = "update_app"

	// maxMetaBytes bounds one configuration change, encoded. Configuration is
	// a handful of short settings; the device downloads all of it.
	maxMetaBytes = 64 << 10

	maxNickLength = 64
)

func init() {
	toolScopes[toolUpdateUserMeta] = writeDeviceScopes
	toolScopes[toolListDeviceTokens] = readDeviceScopes
	toolScopes[toolGetDeviceToken] = readDeviceScopes
	toolScopes[toolUpdateDeviceToken] = updateDeviceScopes
	toolScopes[toolListApps] = readAppScopes
	toolScopes[toolGetApp] = readAppScopes
	toolScopes[toolUpdateApp] = writeAppScopes
}

// changes annotates a tool that overwrites existing data. The destructive hint
// is what makes an MCP client ask the user before every single call; repeating
// a call with the same arguments changes nothing further.
func changes(title string) *sdk.ToolAnnotations {
	destructive := true
	closedWorld := false
	return &sdk.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		IdempotentHint:  true,
		OpenWorldHint:   &closedWorld,
	}
}

func (s *Service) registerManagementTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{
		Name:  toolUpdateUserMeta,
		Title: "Update device configuration",
		Description: "Change the user-meta of a device, the configuration its owner sets. " +
			"Keys in set are added or overwritten, keys in remove are deleted, every other key is kept. " +
			"The device picks the change up on its own. device-meta cannot be changed: only the device reports it.",
		Annotations: changes("Update device configuration"),
	}, s.updateUserMeta)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolListDeviceTokens,
		Title: "List device join tokens",
		Description: "List the active join tokens of the account: what devices use to enrol themselves, " +
			"each with the configuration a device enrolled with it starts out with. " +
			"The token secrets are never shown; tokens are created and disabled in the web app.",
		Annotations: readOnly("List device join tokens"),
	}, s.listDeviceTokens)

	sdk.AddTool(server, &sdk.Tool{
		Name:        toolGetDeviceToken,
		Title:       "Get device join token",
		Description: "Get one active device join token by id, without its secret.",
		Annotations: readOnly("Get device join token"),
	}, s.getDeviceToken)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolUpdateDeviceToken,
		Title: "Update device join token",
		Description: "Rename a device join token, or replace the configuration that devices enrolled with it " +
			"start out with. default_user_meta replaces the whole map. Devices already enrolled are not affected.",
		Annotations: changes("Update device join token"),
	}, s.updateDeviceToken)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolListApps,
		Title: "List applications",
		Description: "List the OAuth applications the account registered, with their client id, type, " +
			"callback URLs and scopes. Client secrets are never shown; applications are created and deleted in the web app.",
		Annotations: readOnly("List applications"),
	}, s.listApps)

	sdk.AddTool(server, &sdk.Tool{
		Name:        toolGetApp,
		Title:       "Get application",
		Description: "Get one registered OAuth application by id, nick or client id, without its secret.",
		Annotations: readOnly("Get application"),
	}, s.getApp)

	sdk.AddTool(server, &sdk.Tool{
		Name:  toolUpdateApp,
		Title: "Update application",
		Description: "Change the display name or the callback URLs of a registered OAuth application. " +
			"redirect_uris replaces the whole list and decides where the application's users are sent after signing in, " +
			"so only set URLs the application's owner controls. Its client id, type, secret and scopes cannot be changed here.",
		Annotations: changes("Update application"),
	}, s.updateApp)
}

// manageError turns an error of these tools into what the assistant should
// read. Validation messages are written for it and pass through; anything else
// is logged and replaced, as toolError does.
func manageError(tool string, err error) error {
	var validation validationError
	if errors.As(err, &validation) {
		return err
	}
	// Somebody else's token or application looks exactly like a missing one.
	if errors.Is(err, errNotFound) {
		return errors.New("nothing by that name or id in this account")
	}
	return toolError(tool, err)
}

// validationError marks a message that is safe and useful to show.
type validationError struct{ msg string }

func (v validationError) Error() string { return v.msg }

func invalid(format string, args ...interface{}) error {
	return validationError{msg: fmt.Sprintf(format, args...)}
}

// update_user_meta

type updateUserMetaInput struct {
	Device string                 `json:"device" jsonschema:"device id or nick"`
	Set    map[string]interface{} `json:"set,omitempty" jsonschema:"keys to add or overwrite, with their new values"`
	Remove []string               `json:"remove,omitempty" jsonschema:"keys to delete"`
}

type updateUserMetaOutput struct {
	Device   deviceSummary          `json:"device"`
	UserMeta map[string]interface{} `json:"user_meta" jsonschema:"the whole configuration after the change"`
}

// userMetaPatch builds the merge document devices.PatchUserMeta takes, where a
// nil value deletes its key.
func userMetaPatch(set map[string]interface{}, remove []string) (map[string]interface{}, error) {
	if len(set) == 0 && len(remove) == 0 {
		return nil, invalid("give at least one key in set or in remove")
	}

	patch := map[string]interface{}{}
	for key, value := range set {
		if strings.TrimSpace(key) == "" {
			return nil, invalid("a key in set is empty")
		}
		// A nil in set would silently delete the key; deleting has its own
		// argument so that it is always deliberate.
		if value == nil {
			return nil, invalid("the value of %q is null; to delete a key name it in remove", key)
		}
		patch[key] = value
	}
	for _, key := range remove {
		if strings.TrimSpace(key) == "" {
			return nil, invalid("a key in remove is empty")
		}
		if _, both := patch[key]; both {
			return nil, invalid("%q is in both set and remove", key)
		}
		patch[key] = nil
	}

	encoded, err := json.Marshal(patch)
	if err != nil {
		return nil, invalid("the values in set cannot be stored")
	}
	if len(encoded) > maxMetaBytes {
		return nil, invalid("the change is %d bytes; the limit is %d", len(encoded), maxMetaBytes)
	}
	return patch, nil
}

func (s *Service) updateUserMeta(ctx context.Context, req *sdk.CallToolRequest, in updateUserMetaInput) (*sdk.CallToolResult, updateUserMetaOutput, error) {
	out := updateUserMetaOutput{}

	caller, err := authorize(req, toolUpdateUserMeta)
	if err != nil {
		return nil, out, err
	}

	patch, err := userMetaPatch(in.Set, in.Remove)
	if err != nil {
		return nil, out, err
	}

	// Resolved against the caller's own devices first, so the write can only
	// ever name a device the caller owns; the write itself is pinned to the
	// owner as well.
	device, err := s.store.resolveDevice(ctx, caller.Prn, in.Device)
	if err != nil {
		return nil, out, manageError(toolUpdateUserMeta, err)
	}
	if err := s.store.patchUserMeta(ctx, caller.Prn, device.ID, patch); err != nil {
		return nil, out, manageError(toolUpdateUserMeta, err)
	}
	log.Printf("INFO: mcp change tool=%s user=%s device=%s set=%d remove=%d",
		toolUpdateUserMeta, caller.Prn, device.ID.Hex(), len(in.Set), len(in.Remove))

	updated, err := s.store.resolveDevice(ctx, caller.Prn, device.ID.Hex())
	if err != nil {
		return nil, out, manageError(toolUpdateUserMeta, err)
	}
	out.Device = summarize(updated)
	out.UserMeta = nonNil(updated.UserMeta)
	return nil, out, nil
}

// device join tokens

type deviceTokenSummary struct {
	ID              string                 `json:"id"`
	Nick            string                 `json:"nick"`
	DefaultUserMeta map[string]interface{} `json:"default_user_meta" jsonschema:"configuration a device enrolled with this token starts out with"`
	TimeCreated     string                 `json:"time_created,omitempty"`
	TimeModified    string                 `json:"time_modified,omitempty"`
}

// summarizeDeviceToken copies the presentable fields one by one. The model also
// has Token and TokenSha; naming what goes out, rather than what stays, is what
// keeps a field added to the model later from leaking by default.
func summarizeDeviceToken(token *utils.PantahubDevicesJoinToken) deviceTokenSummary {
	return deviceTokenSummary{
		ID:              token.ID.Hex(),
		Nick:            token.Nick,
		DefaultUserMeta: nonNil(token.DefaultUserMeta),
		TimeCreated:     formatTime(token.TimeCreated),
		TimeModified:    formatTime(token.TimeModified),
	}
}

type noInput struct{}

type listDeviceTokensOutput struct {
	Tokens []deviceTokenSummary `json:"tokens"`
}

func (s *Service) listDeviceTokens(ctx context.Context, req *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, listDeviceTokensOutput, error) {
	out := listDeviceTokensOutput{Tokens: []deviceTokenSummary{}}

	caller, err := authorize(req, toolListDeviceTokens)
	if err != nil {
		return nil, out, err
	}

	tokens, err := s.store.listDeviceTokens(ctx, caller.Prn)
	if err != nil {
		return nil, out, manageError(toolListDeviceTokens, err)
	}
	for i := range tokens {
		out.Tokens = append(out.Tokens, summarizeDeviceToken(&tokens[i]))
	}
	return nil, out, nil
}

type deviceTokenRef struct {
	Token string `json:"token" jsonschema:"id of the device join token, as list_device_tokens returns it"`
}

func (s *Service) getDeviceToken(ctx context.Context, req *sdk.CallToolRequest, in deviceTokenRef) (*sdk.CallToolResult, deviceTokenSummary, error) {
	caller, err := authorize(req, toolGetDeviceToken)
	if err != nil {
		return nil, deviceTokenSummary{}, err
	}

	token, err := s.store.getDeviceToken(ctx, caller.Prn, in.Token)
	if err != nil {
		return nil, deviceTokenSummary{}, manageError(toolGetDeviceToken, err)
	}
	return nil, summarizeDeviceToken(token), nil
}

type updateDeviceTokenInput struct {
	Token           string                 `json:"token" jsonschema:"id of the device join token"`
	Nick            *string                `json:"nick,omitempty" jsonschema:"new name of the token"`
	DefaultUserMeta map[string]interface{} `json:"default_user_meta,omitempty" jsonschema:"replaces the whole configuration new devices start out with"`
}

func (s *Service) updateDeviceToken(ctx context.Context, req *sdk.CallToolRequest, in updateDeviceTokenInput) (*sdk.CallToolResult, deviceTokenSummary, error) {
	caller, err := authorize(req, toolUpdateDeviceToken)
	if err != nil {
		return nil, deviceTokenSummary{}, err
	}

	if in.Nick == nil && in.DefaultUserMeta == nil {
		return nil, deviceTokenSummary{}, invalid("give a nick, a default_user_meta, or both")
	}
	if in.Nick != nil {
		nick := strings.TrimSpace(*in.Nick)
		if nick == "" || len([]rune(nick)) > maxNickLength {
			return nil, deviceTokenSummary{}, invalid("nick has to be between 1 and %d characters", maxNickLength)
		}
		in.Nick = &nick
	}
	if in.DefaultUserMeta != nil {
		encoded, err := json.Marshal(in.DefaultUserMeta)
		if err != nil || len(encoded) > maxMetaBytes {
			return nil, deviceTokenSummary{}, invalid("default_user_meta cannot be stored or is over %d bytes", maxMetaBytes)
		}
		for key := range in.DefaultUserMeta {
			if strings.TrimSpace(key) == "" {
				return nil, deviceTokenSummary{}, invalid("a key in default_user_meta is empty")
			}
		}
	}

	token, err := s.store.updateDeviceToken(ctx, caller.Prn, in.Token, in.Nick, in.DefaultUserMeta)
	if err != nil {
		return nil, deviceTokenSummary{}, manageError(toolUpdateDeviceToken, err)
	}
	log.Printf("INFO: mcp change tool=%s user=%s token=%s", toolUpdateDeviceToken, caller.Prn, token.ID.Hex())
	return nil, summarizeDeviceToken(token), nil
}

// applications

type appSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Nick         string   `json:"nick"`
	ClientID     string   `json:"client_id" jsonschema:"what the application sends as client_id"`
	Type         string   `json:"type" jsonschema:"public, pkce or confidential"`
	HasSecret    bool     `json:"has_secret" jsonschema:"whether a client secret exists; it is never shown"`
	HasLogo      bool     `json:"has_logo"`
	RedirectURIs []string `json:"redirect_uris"`
	Scopes       []string `json:"scopes"`
	TimeCreated  string   `json:"time_created,omitempty"`
	TimeModified string   `json:"time_modified,omitempty"`
}

// summarizeApp copies the presentable fields one by one, for the same reason
// summarizeDeviceToken does: the model holds the secret hash, and the logo is
// an inline image that can run to hundreds of kilobytes.
func summarizeApp(app *apps.TPApp) appSummary {
	summary := appSummary{
		ID:           app.ID.Hex(),
		Name:         app.Name,
		Nick:         app.Nick,
		ClientID:     app.Prn,
		Type:         app.Type,
		HasSecret:    app.SecretHash != "",
		HasLogo:      app.Logo != "",
		RedirectURIs: app.RedirectURIs,
		Scopes:       []string{},
		TimeCreated:  formatTime(app.TimeCreated),
		TimeModified: formatTime(app.TimeModified),
	}
	if summary.RedirectURIs == nil {
		summary.RedirectURIs = []string{}
	}
	for _, scope := range app.Scopes {
		summary.Scopes = append(summary.Scopes, scope.Service+"/"+scope.ID)
	}
	return summary
}

type listAppsOutput struct {
	Apps []appSummary `json:"apps"`
}

func (s *Service) listApps(ctx context.Context, req *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, listAppsOutput, error) {
	out := listAppsOutput{Apps: []appSummary{}}

	caller, err := authorize(req, toolListApps)
	if err != nil {
		return nil, out, err
	}

	registered, err := s.store.listApps(ctx, caller.Prn)
	if err != nil {
		return nil, out, manageError(toolListApps, err)
	}
	for i := range registered {
		out.Apps = append(out.Apps, summarizeApp(&registered[i]))
	}
	return nil, out, nil
}

type appRef struct {
	App string `json:"app" jsonschema:"id, nick or client id of the application"`
}

func (s *Service) getApp(ctx context.Context, req *sdk.CallToolRequest, in appRef) (*sdk.CallToolResult, appSummary, error) {
	caller, err := authorize(req, toolGetApp)
	if err != nil {
		return nil, appSummary{}, err
	}

	app, err := s.store.getApp(ctx, caller.Prn, in.App)
	if err != nil {
		return nil, appSummary{}, manageError(toolGetApp, err)
	}
	return nil, summarizeApp(app), nil
}

type updateAppInput struct {
	App          string    `json:"app" jsonschema:"id, nick or client id of the application"`
	Name         *string   `json:"name,omitempty" jsonschema:"new display name, shown to users on the consent page"`
	RedirectURIs *[]string `json:"redirect_uris,omitempty" jsonschema:"replaces the whole list of callback URLs; at least one"`
}

func (s *Service) updateApp(ctx context.Context, req *sdk.CallToolRequest, in updateAppInput) (*sdk.CallToolResult, appSummary, error) {
	caller, err := authorize(req, toolUpdateApp)
	if err != nil {
		return nil, appSummary{}, err
	}

	if in.Name == nil && in.RedirectURIs == nil {
		return nil, appSummary{}, invalid("give a name, redirect_uris, or both")
	}
	if strings.TrimSpace(in.App) == "" {
		return nil, appSummary{}, invalid("app is required")
	}

	app, err := s.store.updateApp(ctx, caller.Prn, in.App, apps.Details{Name: in.Name, RedirectURIs: in.RedirectURIs})
	if err != nil {
		// A refusal about the arguments (an empty name, an unusable URL) is
		// worded for whoever sent them. Anything else stays in the logs.
		if errors.Is(err, apps.ErrInvalidDetails) {
			return nil, appSummary{}, invalid("%s", strings.TrimPrefix(err.Error(), apps.ErrInvalidDetails.Error()+": "))
		}
		return nil, appSummary{}, manageError(toolUpdateApp, err)
	}
	log.Printf("INFO: mcp change tool=%s user=%s app=%s name=%v redirect_uris=%v",
		toolUpdateApp, caller.Prn, app.ID.Hex(), in.Name != nil, in.RedirectURIs != nil)
	return nil, summarizeApp(app), nil
}
