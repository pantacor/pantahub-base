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

package auth

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cloudflare/cfssl/revoke"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

const (
	// HTTPHeaderPhClientCertificate pantahub client certificate
	HTTPHeaderPhClientCertificate = "Pantahub-TLS-Client-Cert"

	// HTTPHeaderPhProxyTLSToken pantahub proxy token
	//
	//#nosec G101 -- the name of an HTTP header, not a token value
	HTTPHeaderPhProxyTLSToken = "Pantahub-TLS-Proxy-Token"
)

// handleAuthUsingDeviceCert Get login token using device certificate via tls
// @Summary Get login token using device certificate via tls
// @Description Get login token using device certificate via tls
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags auth
// @Param PhClientCertificate header string true "IDEVID certificate"
// @Success 200 {object} TokenPayload
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/x509/login [post]
func (a *App) handleAuthUsingDeviceCert(c *echo.Context) error {
	cert := tlsProxyCertFilter(c)
	if cert == nil {
		return echoutil.RestErrorWrapper(c, "IDevID need to be used as tls certificate", http.StatusForbidden)
	}

	err := utils.ValidateCaSigned(cert)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "The certificate is can't be trusted", http.StatusForbidden)
	}

	// fail closed: ok == false means the CRL/OCSP responder could not be
	// consulted, so the revocation status is unknown, not "not revoked"
	revoked, ok := revoke.VerifyCertificate(cert)
	if revoked || !ok {
		if !ok {
			log.Printf("WARNING: x509 login: revocation status of %q could not be determined; refusing", cert.Subject.SerialNumber)
		}
		return echoutil.RestErrorWrapper(c, "The certificate is not valid anymore, could be revoked or is expired", http.StatusForbidden)
	}

	deviceID := cert.Subject.SerialNumber

	device, err := devices.GetDeviceByID(c.Request().Context(), deviceID, a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
	}

	token, err := createToken(device)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error creating device token", http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, token)
}

func createToken(device *devices.Device) (*TokenPayload, error) {
	token := jwt.New(jwt.GetSigningMethod("RS256"))
	claims := token.Claims.(jwt.MapClaims)

	timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		return nil, err
	}
	jwtSecretBase64 := utils.GetEnv(utils.EnvPantahubJWTAuthSecret)
	jwtSecretPem, err := base64.StdEncoding.DecodeString(jwtSecretBase64)
	if err != nil {
		return nil, fmt.Errorf("No valid JWT secret (PANTAHUB_JWT_SECRET) in base64 format: %s", err.Error())
	}
	jwtSecret, err := jwt.ParseRSAPrivateKeyFromPEM(jwtSecretPem)
	if err != nil {
		return nil, err
	}

	claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	claims["id"] = device.Prn
	claims["nick"] = device.Nick
	claims["roles"] = "device"
	claims["type"] = "DEVICE"
	claims["prn"] = device.Prn
	claims["owner"] = device.Owner
	claims["scopes"] = "prn:pantahub.com:apis:/base/all"
	claims["orig_iat"] = time.Now().Unix()

	tokenString, err := token.SignedString(jwtSecret)

	return &TokenPayload{Token: tokenString}, err
}

// tlsProxyCertFilter will ensure that calling clients have authenticated with a valid client
// IDevId certificate and will validate the extensions to it.
//
// If validation succeeds the key attributes will be put into the calling context to allow
// business logic to adjust behaviour based on what was found.
//
// This filter can operate in mode behind proxy or directly on TLS port. If we are opreating
// behind a proxy it is mandatory that the proxy authenticates itself to the backend in order
// to enable the code path that uses the "PhClientCertificate" Http header field to retrieve
// the client certificate used.
func tlsProxyCertFilter(c *echo.Context) *x509.Certificate {
	var cert *x509.Certificate

	phProxyTLSUnlockAuth := c.Request().Header.Get(HTTPHeaderPhProxyTLSToken)

	if phProxyTLSUnlockAuth != "" {
		if phProxyTLSUnlockAuth != utils.GetEnv(utils.EnvProxyTLSUnlockAuthToken) {
			_ = echoutil.RestErrorWrapper(c, "invalid proxy tls token configuration", http.StatusInternalServerError)
			return nil
		}
		phCertificate := c.Request().Header.Get(HTTPHeaderPhClientCertificate)
		if phCertificate != "" {
			// Nginx encode the client certificate using url escape instead of hex
			decodedValue, err := url.QueryUnescape(phCertificate)
			if err != nil {
				_ = echoutil.RestErrorWrapper(c, "parse client certificate error", http.StatusInternalServerError)
				return nil
			}

			cert, err = utils.ParsePEMCertString([]byte(decodedValue))
			if err != nil {
				_ = echoutil.RestErrorWrapper(c, "parse client certificate error", http.StatusInternalServerError)
				return nil
			}
		}
		return cert
	} else if c.Request().TLS != nil {
		// if we are NOT behind proxy we extract directlty from TLS connection
		if c.Request().TLS != nil && len(c.Request().TLS.PeerCertificates) == 0 {
			_ = echoutil.RestErrorWrapper(c, "No TLS Certificate available through TLS session", http.StatusInternalServerError)
			return nil
		}
		// PeerCertificates[0] is always the leaf the client proved possession
		// of; the remaining entries are whatever chain it chose to send and
		// must never be used as the identity
		cert = c.Request().TLS.PeerCertificates[0]
		return cert
	}

	return nil
}
