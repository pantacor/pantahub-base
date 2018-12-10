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
package main

import (
	"log"
	"net"
	"net/http"

	"gitlab.com/pantacor/pantahub-base/base"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func main() {

	base.DoInit()

	portInt := utils.GetEnv(utils.ENV_PANTAHUB_PORT_INT)
	portIntTls := utils.GetEnv(utils.ENV_PANTAHUB_PORT_INT_TLS)

	go func() {
		log.Fatal(http.ListenAndServeTLS(":"+portIntTls, "localhost.cert.pem", "localhost.key.pem", nil))
	}()

	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			log.Printf("Serving @ https://" + ip.String() + ":" + portIntTls + "/\n")
			log.Printf("Serving @ http://" + ip.String() + ":" + portInt + "/\n")
		}
	}
	log.Fatal(http.ListenAndServe(":"+portInt, nil))
}
