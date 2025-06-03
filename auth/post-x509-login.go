// Copyright 2016-2020  Pantacor Ltd.
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
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/cloudflare/cfssl/revoke"
	"github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	// HTTPHeaderPhClientCertificate pantahub client certificate
	HTTPHeaderPhClientCertificate = "Pantahub-TLS-Client-Cert"

	// HTTPHeaderPhProxyTLSToken pantahub proxy token
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
func (a *App) handleAuthUsingDeviceCert(w rest.ResponseWriter, r *rest.Request) {
	cert := tlsProxyCertFilter(w, r)
	if cert == nil {
		utils.RestErrorWrapper(w, "IDevID need to be used as tls certificate", http.StatusForbidden)
		return
	}

	err := utils.ValidateCaSigned(cert)
	if err != nil {
		utils.RestErrorWrapper(w, "The certificate is can't be trusted", http.StatusForbidden)
		return
	}

	revoked, ok := revoke.VerifyCertificate(cert)
	if revoked && !ok {
		utils.RestErrorWrapper(w, "The certificate is not valid anymore, could be revoked or is expired", http.StatusForbidden)
		return
	}

	deviceID := cert.Subject.SerialNumber

	device, err := devices.GetDeviceByID(r.Context(), deviceID, a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices"))
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
		return
	}

	token, err := createToken(device)

	w.WriteJson(token)
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
func tlsProxyCertFilter(w rest.ResponseWriter, req *rest.Request) *x509.Certificate {
	var cert *x509.Certificate

	phProxyTLSUnlockAuth := req.Header.Get(HTTPHeaderPhProxyTLSToken)

	if phProxyTLSUnlockAuth != "" {
		if phProxyTLSUnlockAuth != utils.GetEnv(utils.EnvProxyTLSUnlockAuthToken) {
			utils.RestErrorWrapper(w, "invalid proxy tls token configuration", http.StatusInternalServerError)
			return nil
		}
		phCertificate := req.Header.Get(HTTPHeaderPhClientCertificate)
		if phCertificate != "" {
			// Nginx encode the client certificate using url escape instead of hex
			decodedValue, err := url.QueryUnescape(phCertificate)
			if err != nil {
				utils.RestErrorWrapper(w, "parse client certificate error", http.StatusInternalServerError)
				return nil
			}

			cert, err = utils.ParsePEMCertString([]byte(decodedValue))
			if err != nil {
				utils.RestErrorWrapper(w, "parse client certificate error", http.StatusInternalServerError)
				return nil
			}
		}
		return cert
	} else if req.Request.TLS != nil {
		// if we are NOT behind proxy we extract directlty from TLS connection
		if req.Request.TLS != nil && len(req.Request.TLS.PeerCertificates) == 0 {
			utils.RestErrorWrapper(w, "No TLS Certificate available through TLS session", http.StatusInternalServerError)
			return nil
		}
		cert = req.Request.TLS.PeerCertificates[len(req.Request.TLS.PeerCertificates)-1]
		return cert
	}

	return nil
}
