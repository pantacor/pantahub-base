// Copyright 2025  Pantacor Ltd.
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

package storageutils

import (
	"bytes"
	"context"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Repoable interface {
	GetCollectionName() string
	DeleteFile(ctx context.Context, filename string) error
	SaveFile(ctx context.Context, filename, fileType string, file []byte) error
	GetFile(ctx context.Context, id string) ([]byte, int64, error)
	FindBy(ctx context.Context, by, key string, result models.Datable) error
	Find(ctx context.Context, query bson.M, data interface{}, opts ...*options.FindOptions) error
	FindOne(ctx context.Context, query bson.M, data interface{}, opts ...*options.FindOneOptions) error
	FindByID(ctx context.Context, id string, p map[string]interface{}) (map[string]interface{}, error)
	Insert(ctx context.Context, data models.Datable) error
	UpdateOne(ctx context.Context, data models.Datable, upsert bool) error
	UpdateMany(ctx context.Context, query bson.M, updateWith bson.M, opts ...*options.UpdateOptions) error
	CountManyByOwner(ctx context.Context, ownerID string, query map[string]interface{}) (int64, error)
	CountBy(ctx context.Context, query map[string]interface{}) (int64, error)
	BulkWrite(ctx context.Context, operations []mongo.WriteModel, op *options.BulkWriteOptions) error
	SoftDeleteMany(ctx context.Context, q bson.M) error
	DeleteOne(ctx context.Context, data models.Datable) error
	DeleteMany(ctx context.Context, query bson.M) error
	Aggregate(
		ctx context.Context,
		results interface{},
		col string,
		pipeline interface{},
		o *options.AggregateOptions,
	) error
	UpdateWith(ctx context.Context, elementID string, data interface{}) (*mongo.UpdateResult, error)
	UpdateStatus(ctx context.Context, elementID string, status models.OvModeStatus) error
	FindOneAndUpdate(ctx context.Context, query bson.M, update bson.M, result interface{}, opts ...*options.FindOneAndUpdateOptions) error
	HardDeleteOne(ctx context.Context, id string) error
}

// Repo all table DB instances
type Repo struct {
	Storage    *Storage
	Collection *mongo.Collection
}

func (db *Repo) FindOneAndUpdate(ctx context.Context, query bson.M, update bson.M, result interface{}, opts ...*options.FindOneAndUpdateOptions) error {
	return db.Collection.FindOneAndUpdate(ctx, query, update, opts...).Decode(result)
}

func (db *Repo) UpdateStatus(ctx context.Context, elementID string, status models.OvModeStatus) error {
	update := bson.M{
		"$set": bson.M{
			"status": status,
		},
	}
	query := bson.M{"_id": elementID}

	_, err := db.Collection.UpdateOne(ctx, query, update)
	return err
}

func (db *Repo) UpdateWith(ctx context.Context, elementID string, data interface{}) (*mongo.UpdateResult, error) {
	query := bson.M{"_id": elementID}
	update := bson.M{
		"$set": data,
	}

	result, err := db.Collection.UpdateOne(ctx, query, update)
	return result, err
}

func (r *Repo) GetCollectionName() string {
	return r.Collection.Name()
}

func (db *Repo) DeleteFile(ctx context.Context, filename string) error {
	fileID, err := primitive.ObjectIDFromHex(filename)
	if err != nil {
		return err
	}

	fsFiles := db.Storage.GetDatabase().Collection("fs.files")
	_, err = fsFiles.DeleteOne(ctx, bson.M{"filename": filename})
	if err != nil {
		return err
	}

	chucksFiles := db.Storage.GetDatabase().Collection("fs.chucks")

	_, err = chucksFiles.DeleteOne(ctx, bson.M{"files_id": fileID})
	if err != nil {
		return err
	}

	return nil
}

func (db *Repo) SaveFile(ctx context.Context, filename, fileType string, file []byte) error {
	bucket, err := gridfs.NewBucket(db.Storage.GetDatabase())
	if err != nil {
		return err
	}

	uploadOpions := options.GridFSUpload().SetMetadata(bson.M{"Content-Type": fileType})
	uploadStream, err := bucket.OpenUploadStream(filename, uploadOpions)
	if err != nil {
		return err
	}
	defer uploadStream.Close()

	_, err = uploadStream.Write(file)
	if err != nil {
		return err
	}

	return nil
}

func (db *Repo) GetFile(ctx context.Context, id string) ([]byte, int64, error) {
	bucket, err := gridfs.NewBucket(db.Storage.GetDatabase())
	if err != nil {
		return nil, 0, err
	}

	var buf bytes.Buffer
	fileSize, err := bucket.DownloadToStreamByName(id, &buf)
	if err != nil {
		return nil, 0, err
	}

	return buf.Bytes(), fileSize, nil
}

func (db *Repo) FindBy(ctx context.Context, by, key string, result models.Datable) error {
	query := bson.M{"deleted_at": nil}
	query[by] = key
	err := db.Collection.FindOne(ctx, query).Decode(result)
	return err
}

func (db *Repo) Insert(ctx context.Context, data models.Datable) error {
	data.SetCreatedAt()
	data.SetUpdatedAt()
	if data.GetPrn() == "" {
		data.SetPrn(data.GetServicePrn())
	}
	if data.GetOwnerID() != "" && data.GetOwnerPrn() == "" {
		data.SetOwnerPrn(data.GetServicePrn())
	}
	_, err := db.Collection.InsertOne(ctx, data)
	return err
}

func (db *Repo) UpdateOne(ctx context.Context, data models.Datable, upsert bool) error {
	data.SetUpdatedAt()
	if data.GetPrn() == "" {
		data.SetPrn(data.GetServicePrn())
	}
	if data.GetOwnerID() != "" && data.GetOwnerPrn() == "" {
		data.SetOwnerPrn(data.GetServicePrn())
	}

	query := bson.M{"_id": data.GetID()}
	updateOptions := options.Update()
	updateOptions.SetUpsert(upsert)

	update := bson.M{
		"$set": data,
	}

	// // Marshal query and update for logging
	// q, err := json.Marshal(query)
	// if err != nil {
	// 	fmt.Printf("Error marshaling query: %v\n", err)
	// }
	// s, err := json.Marshal(update)
	// if err != nil {
	// 	fmt.Printf("Error marshaling update: %v\n", err)
	// }

	// // Check for logDeployments
	// for _, ld := range logDeployments {
	// 	if strings.Contains(string(q), ld) {
	// 		fmt.Printf("UpdateOne Query: %s\n", q)
	// 		fmt.Printf("UpdateOne Update: %s\n", s)
	// 		break
	// 	}
	// }

	_, err := db.Collection.UpdateOne(
		ctx,
		query,
		update,
		updateOptions,
	)

	return err
}

func (db *Repo) UpdateMany(ctx context.Context, query bson.M, updateWith bson.M, opts ...*options.UpdateOptions) error {
	_, err := db.Collection.UpdateMany(
		ctx,
		query,
		updateWith,
		opts...,
	)

	return err
}

func (db *Repo) CountManyByOwner(ctx context.Context, ownerID string, query map[string]interface{}) (int64, error) {
	if ownerID != "" {
		query["owner_id"] = ownerID
	}
	query["deleted_at"] = nil

	return db.Collection.CountDocuments(ctx, query)
}

func (db *Repo) CountBy(ctx context.Context, query map[string]interface{}) (int64, error) {
	return db.Collection.CountDocuments(ctx, query)
}

func (db *Repo) BulkWrite(ctx context.Context, operations []mongo.WriteModel, op *options.BulkWriteOptions) error {
	_, err := db.Collection.BulkWrite(ctx, operations, op)
	return err
}

func (db *Repo) SoftDeleteMany(ctx context.Context, q bson.M) error {
	now := time.Now()

	updateWith := bson.M{
		"$set": bson.M{
			"deleted_at":   now,
			"status.state": models.StatusDeleted,
		},
	}

	return db.UpdateMany(ctx, q, updateWith)
}

func (db *Repo) HardDeleteOne(ctx context.Context, id string) error {
	query := bson.M{
		"_id": id,
	}

	_, err := db.Collection.DeleteOne(ctx, query)
	return err
}

func (db *Repo) DeleteOne(ctx context.Context, data models.Datable) error {
	data.SetDeletedAt()

	update := bson.M{
		"$set": bson.M{
			"deleted_at": data.GetDeletedAt(),
		},
	}
	query := bson.M{
		"_id":        data.GetID(),
		"deleted_at": nil,
	}

	_, err := db.Collection.UpdateMany(ctx, query, update)
	return err
}

func (db *Repo) DeleteMany(ctx context.Context, query bson.M) error {
	_, err := db.Collection.DeleteMany(ctx, query)
	return err
}

func (db *Repo) Find(ctx context.Context, query bson.M, data interface{}, opts ...*options.FindOptions) error {
	cursor, err := db.Collection.Find(ctx, query, opts...)
	if err != nil {
		return err
	}

	return cursor.All(ctx, data)
}

func (db *Repo) FindOne(ctx context.Context, query bson.M, data interface{}, opts ...*options.FindOneOptions) error {
	decodeable := db.Collection.FindOne(ctx, query, opts...)
	return decodeable.Decode(data)
}

func (db *Repo) FindByID(ctx context.Context, id string, p map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	opts := &options.FindOneOptions{
		Projection: p,
	}
	query := bson.M{"_id": id}

	err := db.FindOne(ctx, query, result, opts)

	return result, err
}

func (db *Repo) Aggregate(
	ctx context.Context,
	results interface{},
	col string,
	pipeline interface{},
	options *options.AggregateOptions,
) error {
	cursor, err := db.Storage.GetCollection(col).Aggregate(ctx, pipeline, options)
	if err != nil {
		return err
	}

	err = cursor.All(ctx, results)
	if err != nil {
		return err
	}

	return err
}

// MergeDefaultProjection merge projection with required values
func MergeDefaultProjection(p map[string]interface{}) map[string]interface{} {
	inclusionProjection := false
	for _, val := range p {
		if val == 1 {
			inclusionProjection = true
			break
		}
	}

	projection := map[string]interface{}{}
	if inclusionProjection {
		projection["_id"] = 1
		projection["created_at"] = 1
		projection["updated_at"] = 1
		projection["deleted_at"] = 1
		projection["owner_id"] = 1
	}

	for key, val := range p {
		projection[key] = val
	}

	return projection
}
