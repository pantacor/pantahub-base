// Copyright 2020  Pantacor Ltd.
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

package profiles

import (
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const pictureMaxSize = 1000 * 1024

// handlePutProfile update profile information for user
// @Summary update profile information for user
// @Description update profile information for user
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body Profile true "Profile payload"
// @Success 200 {object} Profile
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /profiles/ [put]
func (a *App) handlePostProfile(w rest.ResponseWriter, r *rest.Request) {
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "\x89\x50\x4E\x47\x0D\x0A\x1A\x0A", png.Decode, png.DecodeConfig)

	accountPrn := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)
	content, _ := ioutil.ReadAll(r.Body)
	r.Body.Close()

	payload := &UpdateableProfile{}
	err := json.Unmarshal(content, payload)
	if err != nil {
		utils.RestErrorWrapper(w, "Update: "+err.Error(), http.StatusInternalServerError)
		return
	}

	valid, errMsg, userMsg, code := validatePicture(payload.Picture)
	if !valid {
		utils.RestErrorWrapperUser(w, "Update: "+errMsg, userMsg, code)
		return
	}

	newProfile := &Profile{
		UpdateableProfile: payload,
	}

	profile, err := a.updateProfile(accountPrn, newProfile)
	if err != nil {
		utils.RestErrorWrapper(w, "Update: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(profile)
}

func validatePicture(picture string) (bool, string, string, int) {
	if picture == "" {
		return true, "", "", 0
	}

	pictureSize := utils.CalcBinarySize(picture)
	if pictureSize >= pictureMaxSize {
		return false, "Image is to big", "Image is to big", http.StatusPreconditionFailed
	}

	// The actual image starts after the ","
	i := strings.Index(picture, ",")
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(picture[i+1:]))
	config, _, err := image.DecodeConfig(reader)
	if err != nil {
		return false, err.Error(), "Unsupported image format. Supported image format are png, jpeg", http.StatusPreconditionFailed
	}

	if config.Height/config.Width != 1 && config.Height%config.Width != 0 {
		return false, err.Error(), "Image aspect ratio need to be 1", http.StatusPreconditionFailed
	}

	return true, "", "", 0
}
