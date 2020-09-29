//
// Copyright 2017-2010 Pantacor Ltd.
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
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"log"
	"path/filepath"
	"text/template"

	"gopkg.in/mailgun/mailgun-go.v1"
)

type emailData struct {
	Nick  string
	Email string
	Link  string
}

func getMailer() mailgun.Mailgun {
	mgDomain := GetEnv(EnvMailgunDomain)
	mgAPIKey := GetEnv(EnvMailgunAPIKey)
	mgPubAPIKey := GetEnv(EnvMailgunPubAPIKey)

	log.Println("Sending Mail through MAILGUN: " + mgDomain)
	return mailgun.NewMailgun(mgDomain, mgAPIKey, mgPubAPIKey)
}

func getURLPrefix() string {
	urlPrefix := GetEnv(EnvPantahubScheme) + "://" + GetEnv(EnvPantahubWWWHost)
	if GetEnv(EnvPantahubPort) != "" {
		urlPrefix += ":"
		urlPrefix += GetEnv(EnvPantahubPort)
	}

	return urlPrefix
}

// SendResetPasswordEmail send reset password to account
func SendResetPasswordEmail(email, nick, token string) error {
	regEmail := GetEnv(EnvRegEmail)
	link := getURLPrefix() + "/reset_password#token=" + token
	mg := getMailer()

	bodyPlain, err := execTemplate("./tmpl/mails/password_recovery.md", email, nick, link)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	bodyHTML, err := execTemplate("./tmpl/mails/password_recovery.md", email, nick, link)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	message := mg.NewMessage(
		regEmail,
		"Request to reset your password",
		bodyPlain,
		email,
	)

	message.SetHtml(bodyHTML)
	message.AddBCC(regEmail)

	err = addMedias(message)
	if err != nil {
		log.Println("error:", err)
		return nil
	}

	resp, id, err := mg.Send(message)
	if err != nil {
		log.Print(err)
		return err
	}
	log.Printf("ID: %s Resp: %s\n", id, resp)

	return nil
}

// SendWelcome send a verification email
func SendWelcome(email, nick, urlPrefix string) error {
	bodyPlain, err := execTemplate("./tmpl/mails/welcome.md", email, nick, "")
	if err != nil {
		log.Println("error on plain:", err)
		return err
	}

	bodyHTML, err := execTemplate("./tmpl/mails/welcome.html", email, nick, "")
	if err != nil {
		log.Println("error on html:", err)
		return err
	}

	regEmail := GetEnv(EnvRegEmail)
	mg := getMailer()
	message := mg.NewMessage(
		regEmail,
		"Welcome to Pantahub",
		bodyPlain,
		email)

	message.SetHtml(bodyHTML)
	message.AddBCC(regEmail)

	err = addMedias(message)
	if err != nil {
		log.Println("error:", err)
		return nil
	}

	resp, id, err := mg.Send(message)
	if err != nil {
		log.Print(err)
		return err
	}

	log.Printf("ID: %s Resp: %s\n", id, resp)

	return nil
}

// SendVerification send a verification email
func SendVerification(email, nick, id, u string, urlPrefix string) bool {
	link := urlPrefix + "/verify?id=" + id + "&challenge=" + u

	bodyPlain, err := execTemplate("./tmpl/mails/confirm-email.md", email, nick, link)
	if err != nil {
		log.Println("error on plain:", err)
		return false
	}

	bodyHTML, err := execTemplate("./tmpl/mails/confirm-email.html", email, nick, link)
	if err != nil {
		log.Println("error on html:", err)
		return false
	}

	regEmail := GetEnv(EnvRegEmail)
	mg := getMailer()
	message := mg.NewMessage(
		regEmail,
		"Activate your Pantahub account",
		bodyPlain,
		email)

	message.SetHtml(bodyHTML)
	message.AddBCC(regEmail)

	err = addMedias(message)
	if err != nil {
		log.Println("error:", err)
		return false
	}

	resp, id, err := mg.Send(message)
	if err != nil {
		log.Print(err)
		return false
	}

	log.Printf("ID: %s Resp: %s\n", id, resp)

	return true
}

func execTemplate(name, email, nick, link string) (string, error) {
	htmlTemplatePath, _ := filepath.Abs(name)
	t := template.Must(template.ParseFiles(htmlTemplatePath))

	result := new(bytes.Buffer)
	err := t.Execute(result, emailData{
		Email: email,
		Nick:  nick,
		Link:  link,
	})
	return result.String(), err
}

func addMedias(message *mailgun.Message) error {
	logoPng, err := base64.StdEncoding.DecodeString(ImageLogo)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	twitterPng, err := base64.StdEncoding.DecodeString(ImageTwitter)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	linkedinPng, err := base64.StdEncoding.DecodeString(ImageLinkedin)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	rdPng, err := base64.StdEncoding.DecodeString(ImageRd)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	ruPng, err := base64.StdEncoding.DecodeString(ImageRu)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	message.AddReaderInline("logo.png", ioutil.NopCloser(bytes.NewReader(logoPng)))
	message.AddReaderInline("twitter.png", ioutil.NopCloser(bytes.NewReader(twitterPng)))
	message.AddReaderInline("linkedin.png", ioutil.NopCloser(bytes.NewReader(linkedinPng)))
	message.AddReaderInline("rd.png", ioutil.NopCloser(bytes.NewReader(rdPng)))
	message.AddReaderInline("ru.png", ioutil.NopCloser(bytes.NewReader(ruPng)))

	return nil
}
