//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
//
package main

import (
	"log"
	"net"
	"net/http"
	"pantahub-base/base"
	"pantahub-base/utils"
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
