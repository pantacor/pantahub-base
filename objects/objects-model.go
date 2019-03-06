package objects

import (
	"context"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
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

func CalcUsageAfterPost(owner string, mongoClient *mongo.Client,
	objectId string, newSize int64) (*DiskQuotaUsageResult, error) {

	oCol := mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	resp := DiskQuotaUsageResult{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipeline := []bson.M{
		bson.M{"$match": bson.M{"owner": owner}},
		bson.M{
			"$group": bson.M{
				"_id":   "$owner",
				"total": bson.M{"$sum": "$sizeint"},
			},
		},
	}
	pipelineData, err := bson.Marshal(pipeline)
	if err != nil {
		return nil, err
	}
	cur, err := oCol.Aggregate(ctx, pipelineData)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := DiskQuotaUsageResult{}
		err := cur.Decode(&result)
		if err != nil {
			return nil, err
		}
		result.Total += float64(newSize)
		resp = result
		break
	}
	return &resp, nil
}

func CalcUsageAfterPut(owner string, mongoClient *mongo.Client,
	objectId string, newSize int64) (*DiskQuotaUsageResult, error) {

	oCol := mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	resp := DiskQuotaUsageResult{}
	// match all objects, but leave out the one we replace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipeline := []bson.M{
		bson.M{"$match": bson.M{
			"owner": owner,
			"_id":   bson.M{"$ne": objectId},
		}},
		bson.M{
			"$group": bson.M{
				"_id":   "$owner",
				"total": bson.M{"$sum": "$sizeint"},
			},
		},
	}
	pipelineData, err := bson.Marshal(pipeline)
	if err != nil {
		return nil, err
	}
	cur, err := oCol.Aggregate(ctx, pipelineData)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := DiskQuotaUsageResult{}
		err := cur.Decode(&result)
		if err != nil {
			return nil, err
		}
		result.Total += float64(newSize)
		resp = result
		break
	}
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
