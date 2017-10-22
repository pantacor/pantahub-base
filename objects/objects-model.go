package objects

/*
import (
	"context"
)
*/
type Object struct {
	Id          string `json:"id" bson:"id"`
	StorageId   string `json:"storage-id" bson:"_id"`
	Owner       string `json:"owner"`
	ObjectName  string `json:"objectname"`
	Sha         string `json:"sha256sum"`
	Size        string `json:"size"`
	MimeType    string `json:"mime-type"`
	initialized bool
}

type ObjectWithAccess struct {
	Object       `bson:",inline"`
	SignedPutUrl string `json:"signed-puturl"`
	SignedGetUrl string `json:"signed-geturl"`
	Now          string `json:"now"`
	ExpireTime   string `json:"expire-time"`
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
