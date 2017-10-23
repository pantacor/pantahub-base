package trails

import (
	"errors"
	"log"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gopkg.in/mgo.v2/bson"
)

func (a *TrailsApp) isTrailPublic(trailId string) (bool, error) {

	collTrails := a.mgoSession.DB("").C("pantahub_trails")

	if collTrails == nil {
		return false, errors.New("Cannot get collection")
	}

	trail := Trail{}
	log.Println("Trail:" + trailId)
	err := collTrails.Find(bson.M{"_id": bson.ObjectIdHex(trailId)}).One(&trail)

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
