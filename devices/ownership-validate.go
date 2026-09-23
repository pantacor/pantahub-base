//
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

package devices

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// handleValidateOwnership validates the ownership of a device based on its OVMode.
// @Summary Validates device ownership based on OVMode.
// @Description Validates device ownership based on the configured OVMode (TLS, Manual, etc.).
// @Description If OVMode is TLS, the device itself must call this endpoint over a client TLS
// @Description connection whose certificate chains to the token's 'root_of_trust'.
// @Description If OVMode is manual, the device owner (a USER token) calls this endpoint to
// @Description accept the device; the device itself may call it to poll its current status.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "Device ID"
// @Success 200 {object} models.OVModeExtension "Ownership validation successful. Returns OVMode details."
// @Failure 400 {object} utils.RError "Invalid request or parameters."
// @Failure 403 {object} utils.RError "Caller is not allowed to verify this device."
// @Failure 404 {object} utils.RError "Device not found or ownership not verifiable."
// @Failure 500 {object} utils.RError "Internal server error."
// @Router /devices/{id}/ownership/validate [post]
func (a *App) handleValidateOwnership(c *echo.Context) error {
	id := c.Param("id")
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
	}

	if id == "" {
		return echoutil.RestErrorWrapperUser(c, "Invalid device ID", "Invalid device ID", http.StatusBadRequest)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return echoutil.RestErrorWrapperUser(c, "Error with Database connectivity", "Error with Database connectivity", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	device := Device{}
	mDeviceId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, "Invalid device ID format", "Invalid device ID format", http.StatusBadRequest)
	}
	err = collection.FindOne(
		ctx,
		bson.M{"_id": mDeviceId},
	).Decode(&device)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return echoutil.RestErrorWrapperUser(c, "Device not found", "Device not found", http.StatusNotFound)
		} else {
			return echoutil.RestErrorWrapperUser(c, err.Error(), "Error finding device: "+err.Error(), http.StatusInternalServerError)
		}
	}

	if device.OVMode == nil {
		return a.noOvm(c, ctx, &device, jwtPayload)
	}

	if device.OVMode.Status == models.ValidationNotNeeded || device.OVMode.Status == models.Completed {
		return echoutil.WriteJSON(c, http.StatusOK, device.OVMode)
	}

	switch device.OVMode.Mode {
	case models.ManualVerification:
		return a.validateManualOwnership(c, ctx, &device, jwtPayload)
	case models.TLSVerification:
		return a.validateTLSOwnership(c, ctx, &device, jwtPayload)
	default:
		return echoutil.RestErrorWrapperUser(c, "Unsupported OVMode", "Unsupported OVMode", http.StatusBadRequest)
	}
}

func (a *App) noOvm(c *echo.Context, ctx context.Context, device *Device, jwtPayload any) error {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	jwtPayloadIface, ok := jwtPayload.(jwtgo.MapClaims)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
	}

	authID, ok := jwtPayloadIface["prn"].(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	tokenType, ok := jwtPayloadIface["type"].(string)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "JWT Type is not valid", "JWT Type is not valid", http.StatusBadRequest)
	}

	if device.OVMode == nil && device.Owner != "" && tokenType == "DEVICE" && device.Prn == authID {
		device.OVMode = &models.OVModeExtension{
			Status: models.ValidationNotNeeded,
			Mode:   models.DefaultVerification,
		}

		if device.OwnershipUnverify {
			// Reporting the new ownership state to the caller while the write
			// that persists it silently failed would leave the two disagreeing.
			if _, err := collection.UpdateOne(
				ctx,
				bson.M{"prn": device.Prn},
				bson.M{"$set": bson.M{"ovmode": device.OVMode}},
			); err != nil {
				return echoutil.RestErrorWrapper(c, "Error updating device ownership mode: "+err.Error(), http.StatusInternalServerError)
			}
		}
		return echoutil.WriteJSON(c, http.StatusOK, device.OVMode)
	}

	if device.OVMode == nil {
		return echoutil.RestErrorWrapperUser(c, "Device is not claimed yet", "Device is not claimed yet", http.StatusNotFound)
	}

	return nil
}

func (a *App) validateTLSOwnership(c *echo.Context, ctx context.Context, device *Device, jwtPayload any) error {
	if device.OVMode == nil {
		return echoutil.RestErrorWrapperUser(c, "Device does not have OVMode configured", "Device does not have OVMode configured", http.StatusNotFound)
	}

	deviceTokensCollection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	jwtPayloadIface, ok := jwtPayload.(jwtgo.MapClaims)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
	}

	tokenType, ok := jwtPayloadIface["type"].(string)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "JWT Type is not valid", "JWT Type is not valid", http.StatusBadRequest)
	}

	authID, ok := jwtPayloadIface["prn"].(string)
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	if tokenType != "DEVICE" {
		return echoutil.RestErrorWrapperUser(c, "Device can only validate ownership with TLS mode", "Device can only validate ownership with TLS mode", http.StatusBadRequest)
	}

	if authID != device.Prn {
		return echoutil.RestErrorWrapperUser(c, "Device can only validate ownership of it self", "Device can only validate ownership of it self", http.StatusBadRequest)
	}

	if device.OVMode.Mode.IsTLS() && (device.OVMode.TokenID == "" && device.OVMode.RootOfTrust == "") {
		return echoutil.RestErrorWrapperUser(c, "Root of trust is not configured for TLS OVMode", "Root of trust is not configured for TLS OVMode", http.StatusInternalServerError)
	}

	sslClientCert := c.Request().Header.Get("ssl-client-cert")
	if sslClientCert == "" {
		return echoutil.RestErrorWrapperUser(c, "ssl-client-cert header is required for TLS OVMode", "ssl-client-cert header is required for TLS OVMode", http.StatusBadRequest)
	}

	decodedCert, err := url.QueryUnescape(sslClientCert)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to URL decode ssl-client-cert: "+err.Error(), http.StatusBadRequest)
	}

	block, _ := pem.Decode([]byte(decodedCert))
	if block == nil {
		return echoutil.RestErrorWrapperUser(c, "failed to decode PEM block from ssl-client-cert", "failed to decode PEM block from ssl-client-cert", http.StatusBadRequest)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to parse certificate: "+err.Error(), http.StatusBadRequest)
	}

	rootOfTrust := ""
	if device.OVMode.RootOfTrust != "" {
		rootOfTrust = device.OVMode.RootOfTrust
	} else {
		tokenID, err := primitive.ObjectIDFromHex(device.OVMode.TokenID)
		if err != nil {
			return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to parse TokenID to ObjectID: "+err.Error(), http.StatusInternalServerError)
		}
		query := map[string]interface{}{"_id": tokenID}
		deviceToken := utils.PantahubDevicesJoinToken{}
		err = deviceTokensCollection.FindOne(c.Request().Context(), query).Decode(&deviceToken)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return echoutil.RestErrorWrapperUser(c, "Device token not found", "Device token not found for RootOfTrust", http.StatusNotFound)
			} else {
				return echoutil.RestErrorWrapperUser(c, err.Error(), "Error finding device token for RootOfTrust: "+err.Error(), http.StatusInternalServerError)
			}
		}
		rootOfTrust = deviceToken.OVMode.RootOfTrust
	}

	// Load the root certificate (RootOfTrust)
	decodedRootOfTrustBytes, err := base64.StdEncoding.DecodeString(rootOfTrust)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to decode RootOfTrust from base64: "+err.Error(), http.StatusInternalServerError)
	}
	certPool := x509.NewCertPool()

	currentPEMBytes := decodedRootOfTrustBytes
	foundAnyCA := false

	for {
		block, rest := pem.Decode(currentPEMBytes)
		if block == nil {
			break
		}

		if block.Type == "CERTIFICATE" {
			caCert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to parse a certificate from CA chain", http.StatusBadRequest)
			} else {
				certPool.AddCert(caCert)
				foundAnyCA = true
			}
		} else {
			return echoutil.RestErrorWrapperUser(c, "invalid root of trust format", "non-certificate PEM block of type '"+block.Type+"' found in CA file.", http.StatusBadRequest)
		}
		currentPEMBytes = rest
	}

	if !foundAnyCA {
		return echoutil.RestErrorWrapperUser(c, "root of trust contains no valid certificates", "failed to find any valid CERTIFICATE PEM block in CA file (RootOfTrust)", http.StatusInternalServerError)
	}

	opts := x509.VerifyOptions{
		Roots: certPool,
	}

	if _, err := cert.Verify(opts); err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to verify certificate: "+err.Error(), http.StatusForbidden)
	}

	device.OVMode.Status = models.Completed
	device.OVMode.RootOfTrust = rootOfTrust

	_, err = collection.UpdateOne(
		ctx,
		bson.M{"prn": device.Prn},
		bson.M{"$set": bson.M{"ovmode.status": models.Completed}},
	)

	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to update device status: "+err.Error(), http.StatusInternalServerError)
	}

	device.OVMode.RootOfTrust = ""

	return echoutil.WriteJSON(c, http.StatusOK, device.OVMode)
}

// validateManualOwnership handles the manual OVMode. Only the device owner
// (a USER token whose prn matches device.Owner) can complete the verification;
// the device itself may call the endpoint to poll its status, which it needs
// because its credentials are restricted until the owner accepts it.
func (a *App) validateManualOwnership(c *echo.Context, ctx context.Context, device *Device, jwtPayload any) error {
	if device.OVMode == nil {
		return echoutil.RestErrorWrapperUser(c, "Device does not have OVMode configured", "Device does not have OVMode configured", http.StatusNotFound)
	}

	claims, ok := jwtPayload.(jwtgo.MapClaims)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
	}

	callerPrn, ok := claims["prn"].(string)
	if !ok {
		return echoutil.RestErrorWrapperUser(c, "Caller PRN not found in JWT payload", "Caller PRN not found in JWT payload", http.StatusBadRequest)
	}

	tokenType, _ := claims["type"].(string)

	// The device polling its own status: report it, never change it.
	if tokenType == "DEVICE" {
		if callerPrn != device.Prn {
			return echoutil.RestErrorWrapperUser(c, "Device can only query ownership of itself", "Device can only query ownership of itself", http.StatusForbidden)
		}
		device.OVMode.RootOfTrust = ""
		return echoutil.WriteJSON(c, http.StatusOK, device.OVMode)
	}

	if tokenType != "USER" && tokenType != "SESSION" {
		return echoutil.RestErrorWrapperUser(c, "Only the device owner can accept ownership", "Only the device owner can accept ownership", http.StatusForbidden)
	}

	if device.Owner == "" || device.Owner != callerPrn {
		return echoutil.RestErrorWrapperUser(c, "Token PRN does not match device owner", "Token PRN does not match device owner", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	now := time.Now()
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": device.ID},
		bson.M{"$set": bson.M{
			"ovmode.status":        models.Completed,
			"ownership_unverified": false,
			"timemodified":         now,
		}},
	)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "failed to update device status: "+err.Error(), http.StatusInternalServerError)
	}

	device.OVMode.Status = models.Completed
	device.OVMode.RootOfTrust = ""
	return echoutil.WriteJSON(c, http.StatusOK, device.OVMode)
}
