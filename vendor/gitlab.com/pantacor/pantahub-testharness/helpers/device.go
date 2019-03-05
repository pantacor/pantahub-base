//
// Copyright 2018  Pantacor Ltd.
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
package helpers

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-gc/models"
	"gopkg.in/mgo.v2/bson"
)

// DeleteDeviceGarbages : Delete Device Garbages
func DeleteDeviceGarbages(t *testing.T) (map[string]interface{}, *resty.Response) {
	responseData := map[string]interface{}{}
	APIEndPoint := GCAPIUrl + "/devices"
	res, err := resty.R().Delete(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &responseData)
	return responseData, res
}

// MarkDeviceAsGarbage : Mark Device as Garbage
func MarkDeviceAsGarbage(t *testing.T, device models.Device) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := GCAPIUrl + "/markgarbage/device/" + device.ID.Hex()
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// LoginDevice : Login Device
func LoginDevice(
	t *testing.T,
	username string,
	password string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/auth/login"

	res, err := resty.R().SetBody(map[string]string{
		"username": username,
		"password": password,
	}).Post(APIEndPoint)

	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	return response, res
}

// MarkAllUnClaimedDevicesAsGrabage : Mark All UnClaimed Devices As Grabage
func MarkAllUnClaimedDevicesAsGrabage(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := GCAPIUrl + "/markgarbage/devices/unclaimed"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if res.StatusCode() != 200 {
		log.Print(response)
		t.Fail()
	}
	return response, res
}

// UpdateDeviceTimeCreated : Update Device timecreated field
func UpdateDeviceTimeCreated(t *testing.T, device *models.Device) bool {
	TimeLeftForGarbaging := utils.GetEnv("PANTAHUB_GC_UNCLAIMED_EXPIRY")
	duration := ParseDuration(TimeLeftForGarbaging)
	TimeBeforeDuration := time.Now().Local().Add(-duration)
	//log.Print(TimeBeforeDuration)
	TimeBeforeDuration = TimeBeforeDuration.Local().Add(-time.Minute * time.Duration(1)) //decrease 1 min
	//log.Print(TimeBeforeDuration)
	db := db.Session
	c := db.C("pantahub_devices")
	//log.Print("Device id:" + device.ID)

	err := c.Update(
		bson.M{"_id": device.ID},
		bson.M{"$set": bson.M{
			"timecreated": TimeBeforeDuration,
		}})
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
		return false
	}
	return true
}

// DeleteDevice : Delete a Device from database
func DeleteDevice(t *testing.T, device models.Device) bool {
	db := db.Session
	c := db.C("pantahub_devices")
	//log.Print("Device id:" + device.ID)
	err := c.Remove(bson.M{"_id": device.ID})
	if err != nil {
		t.Errorf("Error on Removing: " + err.Error())
		t.Fail()
		return false
	}
	DevicesCount--
	return true
}

// RemoveDevice : Delete Device using base API
func RemoveDevice(
	t *testing.T,
	deviceID string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		Delete(APIEndPoint)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// DeleteAllDevices : Delete All Devices
func DeleteAllDevices(t *testing.T) bool {
	db := db.Session
	c := db.C("pantahub_devices")
	_, err := c.RemoveAll(bson.M{})
	if err != nil {
		t.Errorf("Error on Removing: " + err.Error())
		t.Fail()
		return false
	}
	Devices = []models.Device{}
	DevicesCount = 0
	return true
}

// GetDevice : Get Device Details
func GetDevice(
	t *testing.T,
	deviceID string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		Get(APIEndPoint)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// UpdateDeviceNick : Update Device Nick Name
func UpdateDeviceNick(
	t *testing.T,
	deviceID string,
	newNick string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		SetBody(map[string]string{
			"nick": newNick,
		}).
		Patch(APIEndPoint)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// CreateDevice : Register a Device (As User)
func CreateDevice(t *testing.T, claim bool, secret string) (models.Device, *resty.Response) {
	APIEndPoint := BaseAPIUrl + "/devices/"
	request := resty.R().SetBody(map[string]string{
		"secret": secret,
	})
	if claim {
		request = request.SetAuthToken(UTOKEN)
	}
	res, err := request.Post(APIEndPoint)

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	device := models.Device{}
	err = json.Unmarshal(res.Body(), &device)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	Devices = append(Devices, device)
	DevicesCount++
	return device, res
}

// ProcessDeviceGarbages : Process Device Garbages
func ProcessDeviceGarbages(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := GCAPIUrl + "/processgarbages/devices"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// MakeDevicePublic : Make Device Public
func MakeDevicePublic(
	t *testing.T,
	deviceID string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID + "/public"
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		Put(APIEndPoint)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res

}

// MakeDeviceNonPublic : Make Device Non Public
func MakeDeviceNonPublic(
	t *testing.T,
	deviceID string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID + "/public"
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		Delete(APIEndPoint)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res

}

// ListUserDevices : List User Devices
func ListUserDevices(t *testing.T) (
	[]interface{},
	*resty.Response,
) {
	response := []interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/"
	res, err := resty.R().SetAuthToken(UTOKEN).Get(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// AssignUserToDevice : Assign User To Device
func AssignUserToDevice(
	t *testing.T,
	deviceID string,
	Challenge string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID + "?challenge=" + Challenge
	res, err := resty.R().SetAuthToken(UTOKEN).Put(APIEndPoint)

	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// ParseDuration : Parse Duration referece : https://stackoverflow.com/questions/28125963/golang-parse-time-duration
func ParseDuration(str string) time.Duration {
	durationRegex := regexp.MustCompile(`P(?P<years>\d+Y)?(?P<months>\d+M)?(?P<days>\d+D)?T?(?P<hours>\d+H)?(?P<minutes>\d+M)?(?P<seconds>\d+S)?`)
	matches := durationRegex.FindStringSubmatch(str)

	years := ParseInt64(matches[1])
	months := ParseInt64(matches[2])
	days := ParseInt64(matches[3])
	hours := ParseInt64(matches[4])
	minutes := ParseInt64(matches[5])
	seconds := ParseInt64(matches[6])

	hour := int64(time.Hour)
	minute := int64(time.Minute)
	second := int64(time.Second)
	return time.Duration(years*24*365*hour + months*30*24*hour + days*24*hour + hours*hour + minutes*minute + seconds*second)
}

// ParseInt64 : ParseInt64
func ParseInt64(value string) int64 {
	if len(value) == 0 {
		return 0
	}
	parsed, err := strconv.Atoi(value[:len(value)-1])
	if err != nil {
		return 0
	}
	return int64(parsed)
}

// ChangeDeviceSecret : Change Device Secret
func ChangeDeviceSecret(
	t *testing.T,
	deviceID string,
	newSecret string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID
	request := resty.R().
		SetAuthToken(UTOKEN).
		SetBody(map[string]interface{}{
			"secret": newSecret,
		})
	res, err := request.Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// UpdateUserMetaDetails : Update User Meta Details of a device
func UpdateUserMetaDetails(
	t *testing.T,
	deviceID string,
	details map[string]interface{},
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID + "/user-meta"
	request := resty.R().
		SetAuthToken(UTOKEN).
		SetBody(details)
	res, err := request.Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// UpdateDeviceMetaDetails : Update Device Meta Details
func UpdateDeviceMetaDetails(
	t *testing.T,
	dToken string,
	deviceID string,
	details map[string]interface{},
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/devices/" + deviceID + "/device-meta"
	request := resty.R().
		SetAuthToken(dToken).
		SetBody(details)
	res, err := request.Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}
