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
	"fmt"
	"strconv"

	"gopkg.in/gomail.v2"
)

func SendVerification(email, id, u string, urlPrefix string) bool {

	link := urlPrefix + "/auth/verify?id=" + id + "&challenge=" + u

	host := GetEnv(ENV_SMTP_HOST)
	portStr := GetEnv(ENV_SMTP_PORT)
	user := GetEnv(ENV_SMTP_USER)
	pass := GetEnv(ENV_SMTP_PASS)
	regEmail := GetEnv(ENV_REG_EMAIL)
	port, err := strconv.Atoi(portStr)

	if err != nil {
		fmt.Println("ERROR: Bad port - " + err.Error())
		return false
	}

	body := "A user has requested access. If you want him to get access, send him thef ollowing text with link:" +
		"\n\nTo: " + email + "\n\n\n\nTo verify your account, please click on the link: <a href=\"" + link +
		"\">" + link + "</a><br><br>Best Regards,<br><br>" +
		"A. Sack and R. Mendoza (Pantacor Founders)"

	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", "hubpanta@gmail.com", "Pantahub Registration Desk")
	msg.SetHeader("To", regEmail)
	msg.SetHeader("Subject", "Account Verification <"+email+"> for api.pantahub.com")
	msg.SetBody("text/html", body)
	m := gomail.NewDialer(host, port, user, pass)
	if err := m.DialAndSend(msg); err != nil {
		fmt.Println("ERROR sending email - " + err.Error())
		fmt.Println("Body not sent: \n\t" + body)
		return false
	} else {
		fmt.Println("send message")
	}
	return true
}
