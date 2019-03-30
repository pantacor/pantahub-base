//
// Copyright 2018  Pantacor Ltd.
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

	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-gc/routes"

	"github.com/gorilla/mux"
)

func main() {

	db.Connect()
	router := mux.NewRouter()

	//Mark a device as garbage
	router.HandleFunc("/markgarbage/device/{id}", routes.MarkDeviceAsGarbage).Methods("PUT")

	//Mark all unclaimed devices as garbage after a while(eg: after 5 days)
	router.HandleFunc("/markgarbage/devices/unclaimed", routes.MarkUnClaimedDevicesAsGarbage).Methods("PUT")

	//Mark trails as garbage that lost their parent device
	router.HandleFunc("/markgarbage/trails", routes.MarkAllTrailGarbages).Methods("PUT")

	// Process Device Garbages : find all device documents with gc_processed=false then mark it associated trail as garbages
	router.HandleFunc("/processgarbages/devices", routes.ProcessDeviceGarbages).Methods("PUT")

	// Process Trail Garbages : find all trail documents with gc_processed=false then mark it associated steps & objects as garbages
	router.HandleFunc("/processgarbages/trails", routes.ProcessTrailGarbages).Methods("PUT")

	// Process Step Garbages : find all step documents with gc_processed=false then mark it associated objects as garbages
	router.HandleFunc("/processgarbages/steps", routes.ProcessStepGarbages).Methods("PUT")

	//Delete Garbages of all Devices
	router.HandleFunc("/devices", routes.DeleteDeviceGarbages).Methods("DELETE")

	// Populate used_objects_field for all trails
	router.HandleFunc("/populate/usedobjects/trails", routes.PopulateTrailsUsedObjects).Methods("PUT")

	// Populate used_objects_field for all steps
	router.HandleFunc("/populate/usedobjects/steps", routes.PopulateStepsUsedObjects).Methods("PUT")

	//API Info
	router.HandleFunc("/", routes.APIInfo).Methods("GET")

	go func() {
		log.Fatal(http.ListenAndServeTLS(":2001", "localhost.cert.pem", "localhost.key.pem", router))
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
			log.Printf("Serving @ https://" + ip.String() + ":2001 /\n")
			log.Printf("Serving @ http://" + ip.String() + ":2000 /\n")
		}
	}
	log.Fatal(http.ListenAndServe(":2000", router))
}
