//
// Copyright 2017  Pantacor Ltd.
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
package utils

import (
	"log"

	"gopkg.in/mailgun/mailgun-go.v1"
)

func SendVerification(email, id, u string, urlPrefix string) bool {

	link := urlPrefix + "/auth/verify?id=" + id + "&challenge=" + u

	mgDomain := GetEnv(ENV_MAILGUN_DOMAIN)
	mgApiKey := GetEnv(ENV_MAILGUN_APIKEY)
	mgPubApiKey := GetEnv(ENV_MAILGUN_PUBAPIKEY)
	regEmail := GetEnv(ENV_REG_EMAIL)

	log.Println("Sending Mail through MAILGUN: " + mgDomain)

	body := "A user has requested access. If you want him to get access, send him thef ollowing text with link:" +
		"\n\nTo: " + email + "\n\n\n\nTo verify your account, please click on the link: <a href=\"" + link +
		"\">" + link + "</a><br><br>Best Regards,<br><br>" +
		"A. Sack and R. Mendoza (Pantacor Founders)"

	mg := mailgun.NewMailgun(mgDomain, mgApiKey, mgPubApiKey)
	message := mg.NewMessage(
		"postmaster@pantahub.com",
		"Account Verification <"+email+"> for www.pantahub.com",
		body,
		regEmail)

	resp, id, err := mg.Send(message)

	if err != nil {
		log.Print(err)
		return false
	}
	log.Printf("ID: %s Resp: %s\n", id, resp)

	return true
}
