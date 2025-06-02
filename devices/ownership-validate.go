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
		utils.RestErrorWrapper(w, "JWT Payload is not valid", http.StatusBadRequest)
		return
	}

	if id == "" {
		utils.RestErrorWrapper(w, "Invalid device ID", http.StatusBadRequest)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	device := Device{}
	mDeviceId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid device ID format", http.StatusBadRequest)
		return
	}
	err = collection.FindOne(
		ctx,
		bson.M{"_id": mDeviceId},
	).Decode(&device)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RestErrorWrapper(w, "Device not found", http.StatusNotFound)
		} else {
			utils.RestErrorWrapper(w, "Error finding device: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if device.OVMode == nil {
		utils.RestErrorWrapper(w, "Device does not have OVMode configured", http.StatusNotFound)
		return
	}

	switch device.OVMode.Mode {
	case models.ManualVerification:
		a.validateManualOwnership(w, r, ctx, &device, jwtPayload)
	case models.TLSVerification:
		a.validateTLSOwnership(w, r, ctx, &device, jwtPayload)
	default:
		utils.RestErrorWrapper(w, "Unsupported OVMode", http.StatusBadRequest)
	}
}

func (a *App) validateTLSOwnership(w rest.ResponseWriter, r *rest.Request, ctx context.Context, device *Device, jwtPayload any) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	jwtPayloadIface, ok := jwtPayload.(jwtgo.MapClaims)
	if !ok {
		utils.RestErrorWrapper(w, "JWT Payload is not valid", http.StatusBadRequest)
		return
	}

	tokenType, ok := jwtPayloadIface["type"].(string)
	if !ok {
		utils.RestErrorWrapper(w, "JWT Type is not valid", http.StatusBadRequest)
		return
	}

	if tokenType != "DEVICE" {
		utils.RestErrorWrapper(w, "Device can only validate ownership with TLS mode", http.StatusBadRequest)
		return
	}

	if device.OVMode.Mode.IsTLS() && device.OVMode.RootOfTrust == "" {
		utils.RestErrorWrapper(w, "Root of trust is not configured for TLS OVMode", http.StatusInternalServerError)
		return
	}

	sslClientCert := r.Header.Get("ssl-client-cert")
	if sslClientCert == "" {
		utils.RestErrorWrapper(w, "ssl-client-cert header is required for TLS OVMode", http.StatusBadRequest)
		return
	}

	decodedCert, err := url.QueryUnescape(sslClientCert)
	if err != nil {
		utils.RestErrorWrapper(w, "failed to URL decode ssl-client-cert: "+err.Error(), http.StatusBadRequest)
		return
	}

	block, _ := pem.Decode([]byte(decodedCert))
	if block == nil {
		utils.RestErrorWrapper(w, "failed to decode PEM block from ssl-client-cert", http.StatusBadRequest)
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		utils.RestErrorWrapper(w, "failed to parse certificate: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Load the root certificate (RootOfTrust)
	decodedRootOfTrustBytes, err := base64.StdEncoding.DecodeString(device.OVMode.RootOfTrust)
	if err != nil {
		utils.RestErrorWrapper(w, "failed to decode RootOfTrust from base64: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rootBlock, _ := pem.Decode(decodedRootOfTrustBytes)
	if rootBlock == nil {
		utils.RestErrorWrapper(w, "failed to decode PEM block from RootOfTrust", http.StatusInternalServerError)
		return
	}

	rootCert, err := x509.ParseCertificate(rootBlock.Bytes)
	if err != nil {
		utils.RestErrorWrapper(w, "failed to parse RootOfTrust certificate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Verify the client certificate's signature using the root certificate
	certPool := x509.NewCertPool()
	certPool.AddCert(rootCert)

	opts := x509.VerifyOptions{
		Roots: certPool,
	}

	if _, err := cert.Verify(opts); err != nil {
		utils.RestErrorWrapper(w, "failed to verify certificate: "+err.Error(), http.StatusForbidden)
		return
	}

	device.OVMode.Status = models.Completed

	_, err = collection.UpdateOne(
		ctx,
		bson.M{"prn": device.Prn},
		bson.M{"$set": bson.M{"ovmode.status": models.Completed}},
	)

	if err != nil {
		utils.RestErrorWrapper(w, "failed to update device status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	device.OVMode.RootOfTrust = ""

	w.WriteJson(device.OVMode)
}

func (a *App) validateManualOwnership(w rest.ResponseWriter, r *rest.Request, ctx context.Context, device *Device, jwtPayload any) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	ownerPrn, ok := jwtPayload.(jwtgo.MapClaims)["prn"].(string)

	if !ok {
		utils.RestErrorWrapper(w, "Owner PRN not found in JWT payload", http.StatusBadRequest)
		return
	}

	if device.Owner != ownerPrn {
		utils.RestErrorWrapper(w, "Token PRN does not match device owner", http.StatusForbidden)
		return
	}

	device.OVMode.Status = models.Completed

	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{"prn": device.Prn},
		bson.M{"$set": bson.M{"ovmode.status": models.Completed}},
	)

	if err != nil {
		utils.RestErrorWrapper(w, "failed to update device status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if updateResult.ModifiedCount == 0 {
		utils.RestErrorWrapper(w, "failed to update device status: no document updated", http.StatusInternalServerError)
		return
	}

	w.WriteJson(device.OVMode)
}
