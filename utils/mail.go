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
	"context"
	"encoding/base64"
	"html/template"
	"io"
	"log"
	"path/filepath"
	"time"

	"github.com/mailgun/mailgun-go/v4"
)

type emailData struct {
	Nick   string
	Email  string
	Link   string
	Factor string
}

var mgun mailgun.Mailgun

func getMailer() mailgun.Mailgun {
	if mgun != nil {
		return mgun
	}
	mgDomain := GetEnv(EnvMailgunDomain)

	// if we have legacy settings we continue to init ourselves
	if mgDomain != "" {
		mgAPIKey := GetEnv(EnvMailgunAPIKey)
		// mgPubAPIKey := GetEnv(EnvMailgunPubAPIKey)
		mg := mailgun.NewMailgun(mgDomain, mgAPIKey)
		mgun = mg
	} else {
		mg, err := mailgun.NewMailgunFromEnv()
		if err != nil {
			panic("unable to get mailer " + err.Error())
		}
		mgun = mg
	}

	apiUrl := GetEnv(EnvMailgunApiURL)
	if apiUrl != "" {
		mgun.SetAPIBase(apiUrl)
	}

	return mgun
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

	bodyHTML, err := execTemplate("./tmpl/mails/password_recovery.html", email, nick, link)
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
		log.Println("error adding medias:", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	resp, id, err := mg.Send(ctx, message)
	if err != nil {
		log.Println("error sending email:", err)
		log.Println("error sending email:", resp)
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
		"Welcome to Pantacor Hub",
		bodyPlain,
		email)

	message.SetHtml(bodyHTML)
	message.AddBCC(regEmail)

	err = addMedias(message)
	if err != nil {
		log.Println("error adding medias:", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	resp, id, err := mg.Send(ctx, message)
	if err != nil {
		log.Println("error sending email:", err)
		log.Println("error sending email:", resp)
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
		"Activate your Pantacor Hub account",
		bodyPlain,
		email)

	message.SetHtml(bodyHTML)
	message.AddBCC(regEmail)

	err = addMedias(message)
	if err != nil {
		log.Println("error adding medias:", err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	resp, id, err := mg.Send(ctx, message)
	if err != nil {
		log.Println("error sending email:", err)
		log.Println("error sending email:", resp)
		return false
	}

	log.Printf("ID: %s Resp: %s\n", id, resp)

	return true
}

func execTemplate(name, email, nick, link string) (string, error) {
	return execTemplateData(name, emailData{Email: email, Nick: nick, Link: link})
}

func execTemplateData(name string, data emailData) (string, error) {
	htmlTemplatePath, _ := filepath.Abs(name)
	t, err := template.ParseFiles(htmlTemplatePath)
	if err != nil {
		return "", err
	}

	result := new(bytes.Buffer)
	err = t.Execute(result, data)
	return result.String(), err
}

// SendMFAFactorEnrolled notifies the account holder that a second factor was
// added to their account.
func SendMFAFactorEnrolled(email, nick, factor string) error {
	data := emailData{Email: email, Nick: nick, Factor: factor}
	bodyPlain, err := execTemplateData("./tmpl/mails/mfa-enrolled.md", data)
	if err != nil {
		return err
	}
	bodyHTML, err := execTemplateData("./tmpl/mails/mfa-enrolled.html", data)
	if err != nil {
		return err
	}

	mg := getMailer()
	message := mg.NewMessage(
		GetEnv(EnvRegEmail),
		"A new two-factor method was added to your Pantacor Hub account",
		bodyPlain,
		email)
	message.SetHtml(bodyHTML)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	_, _, err = mg.Send(ctx, message)
	return err
}

func addMedias(message *mailgun.Message) error {
	logoPng, err := base64.StdEncoding.DecodeString(ImageLogo)
	if err != nil {
		log.Println("error:", err)
		return err
	}

	message.AddReaderInline("logo.png", io.NopCloser(bytes.NewReader(logoPng)))
	return nil
}
