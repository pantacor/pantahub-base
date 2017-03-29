//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
//
package main

import (
	"log"
	"net"
	"net/http"
	"pantahub-base/base"
)

func main() {

	base.DoInit()

	go func() {
		log.Fatal(http.ListenAndServeTLS(":12366", "localhost.cert.pem", "localhost.key.pem", nil))
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
			log.Printf("Serving @ https://" + ip.String() + ":12366/\n")
			log.Printf("Serving @ http://" + ip.String() + ":12365/\n")
		}
	}
	log.Fatal(http.ListenAndServe(":12365", nil))
}
