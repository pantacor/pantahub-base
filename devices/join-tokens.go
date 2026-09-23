//
// Copyright 2026 Pantacor Ltd.
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

package devices

import (
	"context"
	"errors"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// #nosec G101 -- a mongo collection name, not a credential
const joinTokensCollection = "pantahub_devices_tokens"

// ErrJoinTokenNotFound covers a token that does not exist, is disabled or
// belongs to somebody else.
var ErrJoinTokenNotFound = errors.New("device token not found")

// joinTokenProjection keeps the secret hash in the database.
var joinTokenProjection = bson.M{"tokensha": 0, "token": 0}

// JoinTokenPatch is what can change on a join token. A nil field is left alone.
type JoinTokenPatch struct {
	Nick            *string
	OVMode          *models.OVModeExtension
	DefaultUserMeta map[string]interface{}
}

func joinTokens(mongoClient *mongo.Client) *mongo.Collection {
	return mongoClient.Database(utils.MongoDb).Collection(joinTokensCollection)
}

// ListJoinTokens returns the owner's active join tokens, oldest first. A limit
// of 0 returns them all.
func ListJoinTokens(ctx context.Context, mongoClient *mongo.Client, owner string, limit int64) ([]utils.PantahubDevicesJoinToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cur, err := joinTokens(mongoClient).Find(ctx,
		bson.M{"owner": owner, "disabled": false},
		options.Find().
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetLimit(limit).
			SetProjection(joinTokenProjection))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	result := []utils.PantahubDevicesJoinToken{}
	if err := cur.All(ctx, &result); err != nil {
		return nil, err
	}
	for i := range result {
		result[i].DefaultUserMeta = utils.BsonUnquoteMap(&result[i].DefaultUserMeta)
	}
	return result, nil
}

// GetJoinToken returns one of the owner's active join tokens.
func GetJoinToken(ctx context.Context, mongoClient *mongo.Client, owner string, id primitive.ObjectID) (*utils.PantahubDevicesJoinToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	token := &utils.PantahubDevicesJoinToken{}
	err := joinTokens(mongoClient).FindOne(ctx,
		bson.M{"_id": id, "owner": owner, "disabled": false},
		options.FindOne().SetProjection(joinTokenProjection)).Decode(token)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrJoinTokenNotFound
	}
	if err != nil {
		return nil, err
	}

	token.DefaultUserMeta = utils.BsonUnquoteMap(&token.DefaultUserMeta)
	return token, nil
}

// PatchJoinToken changes one of the owner's active join tokens and returns it
// as stored afterwards. DefaultUserMeta replaces the whole map.
func PatchJoinToken(ctx context.Context, mongoClient *mongo.Client, owner string, id primitive.ObjectID, patch JoinTokenPatch) (*utils.PantahubDevicesJoinToken, error) {
	set := bson.M{}
	if patch.Nick != nil {
		set["nick"] = *patch.Nick
	}
	if patch.OVMode != nil {
		set["ovmode"] = patch.OVMode
	}
	if patch.DefaultUserMeta != nil {
		// Quoted like at creation, or a dotted key would be read as a path.
		set["defaultusermeta"] = utils.BsonQuoteMap(&patch.DefaultUserMeta)
	}
	if len(set) == 0 {
		return nil, errors.New("nothing to update")
	}
	// The model has no bson tags, so the driver stores TimeModified lowercased.
	set["timemodified"] = time.Now()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	token := &utils.PantahubDevicesJoinToken{}
	err := joinTokens(mongoClient).FindOneAndUpdate(ctx,
		bson.M{"_id": id, "owner": owner, "disabled": false},
		bson.M{"$set": set},
		options.FindOneAndUpdate().
			SetReturnDocument(options.After).
			SetProjection(joinTokenProjection)).Decode(token)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrJoinTokenNotFound
	}
	if err != nil {
		return nil, err
	}

	token.DefaultUserMeta = utils.BsonUnquoteMap(&token.DefaultUserMeta)
	return token, nil
}
