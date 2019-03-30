package helpers

import (
	"encoding/json"
	"testing"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gopkg.in/mgo.v2/bson"
)

// Register : Register user account
func Register(
	t *testing.T,
	email string,
	password string,
	nick string,
) (
	map[string]interface{},
	*resty.Response,
) {
	responseData := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/auth/accounts"
	res, err := resty.R().SetBody(map[string]string{
		"email":    email,
		"password": password,
		"nick":     nick,
	}).Post(APIEndPoint)

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()

	}
	err = json.Unmarshal(res.Body(), &responseData)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return responseData, res
}

// Login : Login user
func Login(
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

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	_, ok := response["token"].(string)
	if ok {
		UTOKEN = response["token"].(string)
	}
	return response, res
}

// VerifyUserAccount : Verify User Account
func VerifyUserAccount(
	t *testing.T,
	account accounts.Account,
) (
	map[string]interface{},
	*resty.Response,
) {
	responseData := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/auth/verify?id=" + account.Id.Hex() + "&challenge=" + account.Challenge
	res, err := resty.R().Get(APIEndPoint)

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &responseData)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return responseData, res
}

// GetUser : Get User Object
func GetUser(t *testing.T, email string) accounts.Account {
	account := accounts.Account{}
	db := db.Session
	c := db.C("pantahub_accounts")
	err := c.Find(bson.M{"email": email}).One(&account)
	if err != nil {
		t.Errorf("Error fetching user record: " + err.Error())
	}
	return account
}

// DeleteAllUserAccounts : Delete All User Accounts
func DeleteAllUserAccounts(t *testing.T) bool {
	db := db.Session
	c := db.C("pantahub_accounts")
	_, err := c.RemoveAll(bson.M{})
	if err != nil {
		t.Errorf("Error on Removing: " + err.Error())
		t.Fail()
		return false
	}
	return true
}

// RefreshToken : Refresh Token
func RefreshToken(t *testing.T, token string) (map[string]interface{}, *resty.Response) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/auth/login"
	res, err := resty.R().
		SetHeader("Authorization", "Bearer "+token).
		Get(APIEndPoint)

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	_, ok := response["token"].(string)
	if ok {
		UTOKEN = response["token"].(string)
	}
	return response, res
}
