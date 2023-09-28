//
// Copyright 2020  Pantacor Ltd.
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

package objects

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"gitlab.com/pantacor/pantahub-base/storagedriver"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

// NewObject makes a new object
func NewObject(shaStr, owner, objectName string) (
	newObject *Object,
	err error,
) {
	newObject = &Object{}

	newObject.ID = shaStr
	newObject.Owner = owner
	newObject.Sha = shaStr
	newObject.ObjectName = objectName

	shabyte, err := utils.DecodeSha256HexString(shaStr)
	if err != nil {
		return newObject, errors.New("Object sha must be a valid sha256:" + err.Error())
	}
	newObject.StorageID = MakeStorageID(owner, shabyte)

	return newObject, nil
}

// SaveObject saves an object
func (a *App) SaveObject(parentCtx context.Context, object *Object, localS3Check bool) (err error) {

	SyncObjectSizes(object)

	var result *DiskQuotaUsageResult
	post := false

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	ctx, cancel := context.WithTimeout(parentCtx, 1*time.Minute)
	defer cancel()

	oldObject := Object{}

	err = collection.FindOne(ctx, bson.M{"_id": object.StorageID}).Decode(&oldObject)
	if err == mongo.ErrNoDocuments {
		post = true
		object.TimeCreated = time.Now()
		object.TimeModified = object.TimeCreated
	} else if err == nil {
		object.TimeCreated = oldObject.TimeCreated
		object.TimeModified = time.Now()
	} else if err != nil {
		return err
	}

	if post {
		ctx, cancel = context.WithTimeout(parentCtx, 1*time.Minute)
		defer cancel()
		result, err = CalcUsageAfterPost(ctx, object.Owner, a.mongoClient, object.ID, object.SizeInt)
		if err != nil {
			return fmt.Errorf("error CalcUsageAfterPost failed: %s", err.Error())
		}
	} else {
		ctx, cancel = context.WithTimeout(parentCtx, 1*time.Minute)
		defer cancel()
		result, err = CalcUsageAfterPut(ctx, object.Owner, a.mongoClient, object.ID, object.SizeInt)
		if err != nil {
			return fmt.Errorf("error CalcUsageAfterPut failed: %s", err.Error())
		}
	}

	ctx, cancel = context.WithTimeout(parentCtx, 1*time.Minute)
	defer cancel()
	quota, err := a.GetDiskQuota(ctx, object.Owner)
	if err != nil {
		return fmt.Errorf("error to calc diskquota: %s", err.Error())
	}

	if result.Total > quota {
		log.Println("Quota exceeded in post object.")
		userError := utils.UserErrorNew("Quota exceeded; delete some objects or request a quota bump from team@pantahub.com")
		return userError
	}

	filePath, err := utils.MakeLocalS3PathForName(object.StorageID)
	if err != nil {
		return errors.New("Error Finding Path for Name" + err.Error())
	}

	if localS3Check {
		sd := storagedriver.FromEnv()
		if sd.Exists(filePath) {
			return ErrObjectS3PathAlreadyExists
		}
	}

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	ctx, cancel = context.WithTimeout(parentCtx, 1*time.Minute)
	defer cancel()
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": object.StorageID},
		bson.M{"$set": object},
		updateOptions,
	)
	if err != nil {
		return errors.New("Error saving object:" + err.Error())
	}
	return nil
}

func (a *App) ResolveObjectWithBacking(pctx context.Context, owner string, sha string) (*Object, error) {
	var hasBackingFile bool

	object := Object{}

	shaBytes, err := utils.DecodeSha256HexString(sha)
	if err != nil {
		return nil, errors.New("state_object: Object sha that could not be decoded from hex:" + err.Error() + " [sha:" + sha + "]")
	}
	// lets use proper storage shas to reflect that fact that each
	// owner has its own copy of the object instance on DB side
	storageID := MakeStorageID(owner, shaBytes)

	err = a.FindObjectByStorageID(pctx, storageID, &object)

	if err == mongo.ErrNoDocuments {
		return nil, err
	} else if object.LinkedObject != "" {
		return nil, nil
	} else if err != nil {
		return nil, errors.New("Unable to find Object by Storage id: " + storageID + " - " + err.Error())
	}

	hasBackingFile, err = HasBackingFile(&object)
	if err != nil {
		return nil, err
	}
	if !hasBackingFile {
		return nil, ErrNoBackingFile
	}

	return &object, nil
}

func (a *App) ResolveObjectWithLinks(ctx context.Context, owner string, sha string, autoLink bool) (*Object, error) {

	var hasBackingFile bool

	object, err := a.ResolveObjectWithBacking(ctx, owner, sha)

	if err != nil && err != mongo.ErrNoDocuments && err != ErrNoBackingFile {
		return nil, err
	}

	if err == nil && object != nil {
		return object, nil
	}

	// otherwise lets do the link dance ourselves
	shaBytes, err := utils.DecodeSha256HexString(sha)
	if err != nil {
		return nil, errors.New("state_object: Object sha that could not be decoded from hex:" + err.Error() + " [sha:" + sha + "]")
	}

	// lets use proper storage shas to reflect that fact that each
	// owner has its own copy of the object instance on DB side
	storageID := MakeStorageID(owner, shaBytes)
	object = new(Object)
	err = a.FindObjectByStorageID(ctx, storageID, object)

	if err == nil && object.LinkedObject == "" {
		hasBackingFile, err = HasBackingFile(object)
		if err != nil {
			return nil, err
		}
	}

	if err != nil && err != mongo.ErrNoDocuments {
		return nil, errors.New("Error finding object by storage id: " + storageID + " - " + err.Error())
	} else if err == mongo.ErrNoDocuments {
		// Make a new object
		if object.Sha == "" {
			object, err = NewObject(sha, owner, "/na/link-for-"+sha)
			if err != nil {
				return nil, errors.New("Error creating object:" + err.Error())
			}
		}
	}

	if autoLink && object.LinkedObject == "" && (err == mongo.ErrNoDocuments || !hasBackingFile) {
		// Link object if there is any public object available
		linked, err2 := a.LinkifyObject(ctx, object)
		if err2 == mongo.ErrNoDocuments {
			return nil, ErrNoLinkTargetAvail
		} else if err2 != nil {
			return nil, errors.New("Error linking object:" + err2.Error())
		} else if !linked {
			if err == mongo.ErrNoDocuments {
				return nil, err
			}
			return nil, ErrNoBackingFile
		}
		log.Printf("Linkified object: %s => %s for sha=%s\n", object.StorageID,
			object.LinkedObject, object.Sha)
	} else if object.LinkedObject == "" && !hasBackingFile {
		return nil, ErrNoBackingFile
	}
	return object, nil
}

func HasBackingFile(object *Object) (bool, error) {
	sd := storagedriver.FromEnv()
	filePath, err := utils.MakeLocalS3PathForName(object.StorageID)
	if err != nil {
		return false, err
	}
	if sd.Exists(filePath) {
		return true, nil
	}
	return false, nil
}

// LinkifyObject checks if there is any public object available to link and link if available
func (a *App) LinkifyObject(ctx context.Context, object *Object) (
	linked bool,
	err error) {

	notOwnedBy := object.Owner

	// Find public object owner from public objects pool
	publicObjectOwner, err := a.FindPublicObjectOwner(ctx, object.Sha, notOwnedBy)
	if err == mongo.ErrNoDocuments {
		return false, err
	} else if err != nil {
		return false, errors.New("Error finding public object owner: " + err.Error())
	}

	publicObject := Object{}
	err = a.FindObjectByShaByOwner(ctx, object.Sha, publicObjectOwner, &publicObject)
	if err != nil {
		return false, errors.New("Error finding object by sha '" + object.Sha + "' & by owner: '" + publicObjectOwner + "'" + err.Error())
	}
	// Link the Object storage-id
	object.LinkedObject = publicObject.StorageID
	object.Size = publicObject.Size
	object.SizeInt = publicObject.SizeInt
	object.MimeType = publicObject.MimeType
	return true, nil
}

// GetObjectWithAccess returns an ObjectWithAccess instance
func GetObjectWithAccess(object Object, endPoint string) *ObjectWithAccess {

	issuerURL := utils.GetAPIEndpoint(endPoint)
	newObjectWithAccess := MakeObjAccessible(issuerURL, object.Owner, object, object.StorageID)

	return &newObjectWithAccess
}

// FindPublicObjectOwner is to check if the object is used in any of the public steps, if yes return the owner string
func (a *App) FindPublicObjectOwner(parentCtx context.Context, sha string, notOwnedBy string) (
	ownerStr string,
	err error,
) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_public_steps")

	ctx, cancel := context.WithTimeout(parentCtx, 1*time.Minute)
	defer cancel()

	publicStep := map[string]interface{}{}

	query := bson.M{
		"object_sha": sha,
		"ispublic":   true,
	}
	if notOwnedBy != "" {
		query["owner"] = bson.M{"$ne": notOwnedBy}
	}
	err = collection.FindOne(ctx, query).Decode(&publicStep)
	if err != nil {
		return "", err
	}

	return publicStep["owner"].(string), nil
}

// FindObjectByShaByOwner is to find object by sha & by owner
func (a *App) FindObjectByShaByOwner(
	parentCtx context.Context,
	Sha, Owner string,
	obj *Object,
) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	ctx, cancel := context.WithTimeout(parentCtx, 1*time.Minute)
	defer cancel()

	err := collection.FindOne(ctx, bson.M{
		"id":    Sha,
		"owner": Owner,
		"$or": []bson.M{
			{"linked_object": nil},
			{"linked_object": ""},
		},
	}).Decode(&obj)

	return err
}
