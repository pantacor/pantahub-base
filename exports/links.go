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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// A download link lets whoever holds it fetch one export, the same archive GET
// /exports/:owner/:nick/:rev/:filename returns, without a login: a browser
// opening it from an assistant's answer, or curl. The link carries a short
// lived token for that one export and nothing else.

const (
	// LinkPath is where download links are served, under the API.
	LinkPath = "/exports/links"

	// MaxLinkTTL bounds how long a link can be valid.
	MaxLinkTTL = 30 * time.Minute

	claimLinkOwnerNick = "own"
	claimLinkDevice    = "dev"
	claimLinkRev       = "rev"
	claimLinkParts     = "parts"
)

// LinkRequest names the export a link opens.
type LinkRequest struct {
	OwnerPrn   string
	OwnerNick  string
	DeviceNick string
	Rev        int
	// Parts narrows the export to these parts, as the parts query parameter
	// of the export endpoint does. Empty exports the whole revision.
	Parts []string
}

var linkKeys = loadLinkKeys()

// loadLinkKeys returns a fresh once-loader of the API's keys; tests swap it.
func loadLinkKeys() func() (*utils.JwtRsaKeys, error) {
	return sync.OnceValues(func() (*utils.JwtRsaKeys, error) {
		return utils.GetJwtRsaKeys("", "")
	})
}

// linkAudience is a URL on purpose: every other consumer of tokens signed
// with this key refuses one bound to a URL (utils.IsResourceBoundAudience), so
// a link token cannot be used as a login anywhere.
func linkAudience() string {
	return utils.GetAPIEndpoint(LinkPath)
}

// SignDownloadLink returns a URL that downloads the export of req for ttl.
func SignDownloadLink(req LinkRequest, ttl time.Duration) (string, time.Time, error) {
	if req.OwnerPrn == "" || req.OwnerNick == "" || req.DeviceNick == "" || req.Rev < 0 {
		return "", time.Time{}, errors.New("a link names an owner, a device and a revision")
	}
	if ttl <= 0 || ttl > MaxLinkTTL {
		ttl = MaxLinkTTL
	}
	keys, err := linkKeys()
	if err != nil {
		return "", time.Time{}, err
	}

	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	expires := now.Add(ttl)
	token := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud":              linkAudience(),
		"iss":              utils.GetAPIEndpoint(""),
		"sub":              req.OwnerPrn,
		"jti":              hex.EncodeToString(id),
		"iat":              now.Unix(),
		"exp":              expires.Unix(),
		claimLinkOwnerNick: req.OwnerNick,
		claimLinkDevice:    req.DeviceNick,
		claimLinkRev:       req.Rev,
		claimLinkParts:     strings.Join(req.Parts, ","),
	})
	signed, err := token.SignedString(keys.PrivateKey)
	if err != nil {
		return "", time.Time{}, err
	}

	filename := url.PathEscape(req.DeviceNick + "-" + strconv.Itoa(req.Rev) + ".tar.gz")
	return utils.GetAPIEndpoint(LinkPath + "/" + signed + "/" + filename), expires, nil
}

// handleGetExportLink serves the export a download link names.
// @Summary Download an export through a signed link
// @Description Downloads the export named by a link from SignDownloadLink, without a login. Links are short lived.
// @Produce  application/gzip
// @Tags exports
// @Param token path string true "Link token"
// @Param filename path string true "File name"
// @Success 200 {binary} []byte
// @Failure 403 {object} utils.RError
// @Router /exports/links/{token}/{filename} [get]
func (a *App) handleGetExportLink(c *echo.Context) error {
	claims, err := parseLinkToken(c.Param("token"))
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "This download link is not valid or has expired", http.StatusForbidden)
	}

	owner, _ := claims["sub"].(string)
	ownerNick, _ := claims[claimLinkOwnerNick].(string)
	deviceNick, _ := claims[claimLinkDevice].(string)
	parts, _ := claims[claimLinkParts].(string)
	rev, ok := claims[claimLinkRev].(float64)
	if owner == "" || ownerNick == "" || deviceNick == "" || !ok || rev < 0 {
		return echoutil.RestErrorWrapperUser(c, "incomplete link token", "This download link is not valid or has expired", http.StatusForbidden)
	}

	// The owner reads their own device, as they would when logged in.
	return a.serveExport(c, ownerNick, deviceNick, strconv.Itoa(int(rev)), c.Param("filename"), parts, false, owner, "USER")
}

func parseLinkToken(raw string) (jwtgo.MapClaims, error) {
	keys, err := linkKeys()
	if err != nil {
		return nil, err
	}
	token, err := jwtgo.Parse(raw, func(t *jwtgo.Token) (interface{}, error) {
		if t.Method != jwtgo.SigningMethodRS256 {
			return nil, errors.New("unexpected signing method")
		}
		return keys.PublicKey, nil
	}, jwtgo.WithAudience(linkAudience()), jwtgo.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired download link")
	}
	claims, ok := token.Claims.(jwtgo.MapClaims)
	if !ok {
		return nil, errors.New("unreadable download link")
	}
	return claims, nil
}
