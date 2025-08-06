//
// Copyright 2025  Pantacor Ltd.
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
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// handleValidateOwnership validates the ownership of a device based on its OVMode.
// @Summary Validates device ownership based on OVMode.
// @Description Validates device ownership based on the configured OVMode (TLS, Manual, etc.).
// @Description If OVMode is TLS, the client must use the 'root_of_trust' as the client TLS connection.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {object} models.OVModeExtension "Ownership validation successful. Returns OVMode details."
// @Failure 400 {object} utils.RError "Invalid request or parameters."
// @Failure 404 {object} utils.RError "Device not found or ownership not verifiable."
// @Failure 500 {object} utils.RError "Internal server error."
// @Router /devices/{id}/ownership/validate [get]
func (a *App) handleValidateOwnership(w rest.ResponseWriter, r *rest.Request) {
	for name, values := range r.Header {
		for _, value := range values {
			log.Printf("%s: %s", name, value)
		}
	}
	id := r.PathParam("id")
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapperUser(w, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
		return
	}

	if id == "" {
		utils.RestErrorWrapperUser(w, "Invalid device ID", "Invalid device ID", http.StatusBadRequest)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapperUser(w, "Error with Database connectivity", "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	device := Device{}
	mDeviceId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		utils.RestErrorWrapperUser(w, "Invalid device ID format", "Invalid device ID format", http.StatusBadRequest)
		return
	}
	err = collection.FindOne(
		ctx,
		bson.M{"_id": mDeviceId},
	).Decode(&device)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RestErrorWrapperUser(w, "Device not found", "Device not found", http.StatusNotFound)
		} else {
			utils.RestErrorWrapperUser(w, err.Error(), "Error finding device: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if device.OVMode == nil {
		a.noOvm(w, r, ctx, &device, jwtPayload)
		return
	}

	if device.OVMode.Status == models.ValidationNotNeeded || device.OVMode.Status == models.Completed {
		w.WriteJson(device.OVMode)
		return
	}

	switch device.OVMode.Mode {
	case models.ManualVerification:
		a.validateManualOwnership(w, r, ctx, &device, jwtPayload)
		return
	case models.TLSVerification:
		a.validateTLSOwnership(w, r, ctx, &device, jwtPayload)
		return
	default:
		utils.RestErrorWrapperUser(w, "Unsupported OVMode", "Unsupported OVMode", http.StatusBadRequest)
	}
}

func (a *App) noOvm(w rest.ResponseWriter, r *rest.Request, ctx context.Context, device *Device, jwtPayload any) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	jwtPayloadIface, ok := jwtPayload.(jwtgo.MapClaims)
	if !ok {
		utils.RestErrorWrapperUser(w, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
		return
	}

	authID, ok := jwtPayloadIface["prn"].(string)
	if !ok {
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	tokenType, ok := jwtPayloadIface["type"].(string)
	if !ok {
		utils.RestErrorWrapperUser(w, "JWT Type is not valid", "JWT Type is not valid", http.StatusBadRequest)
		return
	}

	if device.OVMode == nil && device.Owner != "" && tokenType == "DEVICE" && device.Prn == authID {
		device.OVMode = &models.OVModeExtension{
			Status: models.ValidationNotNeeded,
			Mode:   models.DefaultVerification,
		}

		if device.OwnershipUnverify {
			collection.UpdateOne(
				ctx,
				bson.M{"prn": device.Prn},
				bson.M{"$set": bson.M{"ovmode": device.OVMode}},
			)
		}
		w.WriteJson(device.OVMode)
		return
	}

	if device.OVMode == nil {
		utils.RestErrorWrapperUser(w, "Device is not claimed yet", "Device is not claimed yet", http.StatusNotFound)
		return
	}
}

func (a *App) validateTLSOwnership(w rest.ResponseWriter, r *rest.Request, ctx context.Context, device *Device, jwtPayload any) {
	if device.OVMode == nil {
		utils.RestErrorWrapperUser(w, "Device does not have OVMode configured", "Device does not have OVMode configured", http.StatusNotFound)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	jwtPayloadIface, ok := jwtPayload.(jwtgo.MapClaims)
	if !ok {
		utils.RestErrorWrapperUser(w, "JWT Payload is not valid", "JWT Payload is not valid", http.StatusBadRequest)
		return
	}

	tokenType, ok := jwtPayloadIface["type"].(string)
	if !ok {
		utils.RestErrorWrapperUser(w, "JWT Type is not valid", "JWT Type is not valid", http.StatusBadRequest)
		return
	}

	authID, ok := jwtPayloadIface["prn"].(string)
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	if tokenType != "DEVICE" {
		utils.RestErrorWrapperUser(w, "Device can only validate ownership with TLS mode", "Device can only validate ownership with TLS mode", http.StatusBadRequest)
		return
	}

	if authID != device.Prn {
		utils.RestErrorWrapperUser(w, "Device can only validate ownership of it self", "Device can only validate ownership of it self", http.StatusBadRequest)
		return
	}

	if device.OVMode.Mode.IsTLS() && device.OVMode.RootOfTrust == "" {
		utils.RestErrorWrapperUser(w, "Root of trust is not configured for TLS OVMode", "Root of trust is not configured for TLS OVMode", http.StatusInternalServerError)
		return
	}

	sslClientCert := r.Header.Get("ssl-client-cert")
	if sslClientCert == "" {
		utils.RestErrorWrapperUser(w, "ssl-client-cert header is required for TLS OVMode", "ssl-client-cert header is required for TLS OVMode", http.StatusBadRequest)
		return
	}

	decodedCert, err := url.QueryUnescape(sslClientCert)
	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "failed to URL decode ssl-client-cert: "+err.Error(), http.StatusBadRequest)
		return
	}

	block, _ := pem.Decode([]byte(decodedCert))
	if block == nil {
		utils.RestErrorWrapperUser(w, "failed to decode PEM block from ssl-client-cert", "failed to decode PEM block from ssl-client-cert", http.StatusBadRequest)
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "failed to parse certificate: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Load the root certificate (RootOfTrust)
	decodedRootOfTrustBytes, err := base64.StdEncoding.DecodeString(device.OVMode.RootOfTrust)
	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "failed to decode RootOfTrust from base64: "+err.Error(), http.StatusInternalServerError)
		return
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
				utils.RestErrorWrapperUser(w, err.Error(), "failed to parse a certificate from CA chain", http.StatusBadRequest)
				return
			} else {
				certPool.AddCert(caCert)
				foundAnyCA = true
			}
		} else {
			utils.RestErrorWrapperUser(w, "invalid root of trust format", "non-certificate PEM block of type '"+block.Type+"' found in CA file.", http.StatusBadRequest)
			return
		}
		currentPEMBytes = rest
	}

	if !foundAnyCA {
		utils.RestErrorWrapperUser(w, "root of trust contains no valid certificates", "failed to find any valid CERTIFICATE PEM block in CA file (RootOfTrust)", http.StatusInternalServerError)
		return
	}

	opts := x509.VerifyOptions{
		Roots: certPool,
	}

	if _, err := cert.Verify(opts); err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "failed to verify certificate: "+err.Error(), http.StatusForbidden)
		return
	}

	device.OVMode.Status = models.Completed

	_, err = collection.UpdateOne(
		ctx,
		bson.M{"prn": device.Prn},
		bson.M{"$set": bson.M{"ovmode.status": models.Completed}},
	)

	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "failed to update device status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	device.OVMode.RootOfTrust = ""

	w.WriteJson(device.OVMode)
}

func (a *App) validateManualOwnership(w rest.ResponseWriter, r *rest.Request, ctx context.Context, device *Device, jwtPayload any) {
	if device.OVMode == nil {
		utils.RestErrorWrapperUser(w, "Device does not have OVMode configured", "Device does not have OVMode configured", http.StatusNotFound)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	ownerPrn, ok := jwtPayload.(jwtgo.MapClaims)["prn"].(string)

	if !ok {
		utils.RestErrorWrapperUser(w, "Owner PRN not found in JWT payload", "Owner PRN not found in JWT payload", http.StatusBadRequest)
		return
	}

	if device.Owner != ownerPrn {
		utils.RestErrorWrapperUser(w, "Token PRN does not match device owner", "Token PRN does not match device owner", http.StatusForbidden)
		return
	}

	device.OVMode.Status = models.Completed

	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{"prn": device.Prn},
		bson.M{"$set": bson.M{"ovmode.status": models.Completed}},
	)

	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "failed to update device status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if updateResult.ModifiedCount == 0 {
		utils.RestErrorWrapperUser(w, "failed to update device status: no document updated", "failed to update device status: no document updated", http.StatusInternalServerError)
		return
	}

	w.WriteJson(device.OVMode)
}
