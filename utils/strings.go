package utils

import (
	"strings"

	"github.com/asaskevich/govalidator"
)

// XXX: make this a nice prn helper tool
func PrnGetId(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1 : len(prn)]
}

func IsNick(nick string) bool {
	l := len(nick)
	if l > 3 && l < 24 {
		return true
	}
	return false
}

func IsEmail(email string) bool {
	return govalidator.IsEmail(email)
}
