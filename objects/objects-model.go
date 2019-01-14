package objects

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type Object struct {
	Id          string `json:"id" bson:"id"`
	StorageId   string `json:"storage-id" bson:"_id"`
	Owner       string `json:"owner"`
	ObjectName  string `json:"objectname"`
	Sha         string `json:"sha256sum"`
	Size        string `json:"size"`
	SizeInt     int64  `json:"sizeint"`
	MimeType    string `json:"mime-type"`
	initialized bool
	Garbage     bool `json:"garbage" bson:"garbage"`
}

type ObjectWithAccess struct {
	Object       `bson:",inline"`
	SignedPutUrl string `json:"signed-puturl"`
	SignedGetUrl string `json:"signed-geturl"`
	Now          string `json:"now"`
	ExpireTime   string `json:"expire-time"`
}

type DiskQuotaUsageResult struct {
	Id    string  `json:"id" bson:"_id"`
	Total float64 `json:"total"`
}

func CalcUsageAfterPost(owner string, mgoSession *mgo.Session,
	objectId bson.ObjectId, newSize int64) (*DiskQuotaUsageResult, error) {

	oCol := mgoSession.DB("").C("pantahub_objects")
	resp := DiskQuotaUsageResult{}
	err := oCol.Pipe([]bson.M{{"$match": bson.M{"owner": owner}},
		{"$group": bson.M{"_id": "$owner", "total": bson.M{"$sum": "$sizeint"}}}}).One(&resp)

	if err != nil {
		// we bail if we receive any error, but ErrNotFound which happens if user
		// does not own any objects yet
		if err != mgo.ErrNotFound {
			return nil, err
		}
	}

	resp.Total = resp.Total + float64(newSize)

	return &resp, nil
}

func CalcUsageAfterPut(owner string, mgoSession *mgo.Session,
	objectId bson.ObjectId, newSize int64) (*DiskQuotaUsageResult, error) {

	oCol := mgoSession.DB("").C("pantahub_objects")
	resp := DiskQuotaUsageResult{}
	// match all objects, but leave out the one we replace
	err := oCol.Pipe([]bson.M{{"$match": bson.M{"owner": owner, "_id": bson.M{"$ne": objectId}}},
		{"$group": bson.M{"_id": "$owner", "total": bson.M{"$sum": "$sizeint"}}}}).One(&resp)

	if err != nil {
		return nil, err
	}

	resp.Total = resp.Total + float64(newSize)

	return &resp, nil
}

/*
func (o *Object) FindById(ctx context.Context, objectId string) {
	ctx.Value(OBJECTS_ACCESS_PRINCIPAL)
}

func (o *Object) Reload(ctx context.Context) {
}

type Page struct {
	Start  int
	Size   int
	Len    int
	Length int
	Data   []interface{}
}

type Objects []Object

func (o *Objects) FindColl(objectId string, filter map[string]interface{}) {
}

func (o *Objects) FindColl(objectId string, filter map[string]interface{}, start, size int) {
}
*/
