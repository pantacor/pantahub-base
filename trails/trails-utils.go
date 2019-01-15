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
package trails

import (
	"errors"
	"log"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gopkg.in/mgo.v2/bson"
)

func (a *TrailsApp) isTrailPublic(trailID string) (bool, error) {

	collTrails := a.mgoSession.DB("").C("pantahub_trails")

	if collTrails == nil {
		return false, errors.New("Cannot get collection")
	}

	trail := Trail{}
	log.Println("Trail:" + trailID)
	err := collTrails.FindId(bson.ObjectIdHex(trailID)).One(&trail)

	if err != nil {
		return false, err
	}

	collDevices := a.mgoSession.DB("").C("pantahub_devices")

	if collDevices == nil {
		return false, errors.New("Cannot get collection2")
	}

	device := devices.Device{}
	err = collDevices.Find(bson.M{"prn": trail.Device}).One(&device)

	if err != nil {
		return false, err
	}

	return device.IsPublic, nil
}
