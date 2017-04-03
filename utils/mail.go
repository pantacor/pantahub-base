//
// Copyright 2017  Alexander Sack <asac129@gmail.com>
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
	port, err := strconv.Atoi(portStr)

	if err != nil {
		fmt.Println("ERROR: Bad port - " + err.Error())
		return false
	}

	body := "To verify your account, please click on the link: <a href=\"" + link +
		"\">" + link + "</a><br><br>Best Regards,<br><br>" +
		"A. Sack and R. Mendoza (Pantacor Founders)"

	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", "hubpanta@gmail.com", "Pantahub Registration Desk")
	msg.SetHeader("To", email)
	msg.SetHeader("Subject", "Account Verification for api.pantahub.com")
	msg.SetBody("text/html", body)
	m := gomail.NewDialer(host, port, user, pass)
	if err := m.DialAndSend(msg); err != nil {
		fmt.Println("ERROR sending email - " + err.Error())
		fmt.Println("Body not sent: \n\t" + body)
		return false
	}
	return true
}
