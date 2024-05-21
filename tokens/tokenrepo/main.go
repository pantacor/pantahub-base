// Copyright 2024  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package tokenrepo

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/tokens/tokenmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/querymongo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
)

const (
	// DBCollection db collection name for user tokens
	DBCollection = "pantahub_auth_tokens"

	// Prn name convection for the prn
	Prn = "prn:::tokens:/"
)

var searchableQueries = map[string]bool{
	"owner":      true,
	"_id":        true,
	"created_at": true,
}

type Repo struct {
	mongoClient *mongo.Client
	db          *mongo.Database
	col         *mongo.Collection
}

type RepoWriteOpts struct {
	Upsert bool
}

func New(mongoClient *mongo.Client) *Repo {
	db := mongoClient.Database(utils.MongoDb)
	return &Repo{
		mongoClient: mongoClient,
		db:          db,
		col:         db.Collection(DBCollection),
	}
}

// SetIndexes sets up indexes in the repository collection.
//
// No parameters.
// Return type: error.
func (r *Repo) SetIndexes() error {
	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection := r.mongoClient.Database(utils.MongoDb).Collection(DBCollection)
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", DBCollection, err.Error())
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = r.mongoClient.Database(utils.MongoDb).Collection(DBCollection)
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", DBCollection, err.Error())
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = r.mongoClient.Database(utils.MongoDb).Collection(DBCollection)
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", DBCollection, err.Error())
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner", Value: 1},
			{Key: "name", Value: 1},
			{Key: "deleted_at", Value: 1},
		},
		Options: &indexOptions,
	}
	collection = r.mongoClient.Database(utils.MongoDb).Collection(DBCollection)
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", DBCollection, err.Error())
	}

	return nil
}

// SaveToken saves the authentication token in the repository.
//
// Parameters:
// - ctx: the context in which the function is executed
// - token: the authentication token to save
// - opts: optional parameters for write operations
// Return type: error
func (r *Repo) SaveToken(ctx context.Context, token *tokenmodels.AuthToken, opts ...RepoWriteOpts) error {
	if token.ID.IsZero() {
		token.ID = primitive.NewObjectID()
		token.Prn = utils.IDGetPrn(token.ID, "auth-tokens")
	}

	upsert := false
	if opts != nil {
		upsert = opts[0].Upsert
	}

	mongoOpts := options.UpdateOptions{
		Upsert: &upsert,
	}

	query := bson.M{"_id": token.ID, "owner": token.Owner}
	updated := bson.M{"$set": token}
	_, err := r.col.UpdateOne(ctx, query, updated, &mongoOpts)
	if err != nil {
		return err
	}

	return err
}

// Create saves the authentication token in the repository.
//
// Parameters:
// - ctx: the context in which the function is executed
// - token: the authentication token to save
// - opts: optional parameters for write operations
// Return type: error
func (r *Repo) Create(ctx context.Context, token *tokenmodels.AuthToken, opts ...RepoWriteOpts) error {
	if token.ID.IsZero() {
		token.ID = primitive.NewObjectID()
		token.Prn = utils.IDGetPrn(token.ID, "auth-tokens")
	}

	_, err := r.col.InsertOne(ctx, token, nil)
	if err != nil {
		return err
	}

	return err
}

func (r *Repo) DeleteToken(ctx context.Context, id string) error {
	objectid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	query := bson.M{"_id": objectid}
	update := bson.M{
		"$set": bson.M{
			"deleted_at": time.Now(),
			"deleted":    true,
		},
	}

	_, err = r.col.UpdateOne(ctx, query, update, nil)
	if err != nil {
		return err
	}

	return nil
}

// GetToken retrieves an authentication token by ID.
//
// Parameters:
// - ctx: the context in which the function is executed
// - id: the unique identifier of the token
// Return type:
// - *tokenmodels.AuthToken: the retrieved token information
// - error: an error if the operation fails
func (r *Repo) GetToken(ctx context.Context, id string) (*tokenmodels.AuthToken, error) {
	objectid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	query := bson.M{"_id": objectid}
	token := &tokenmodels.AuthToken{}
	decodeable := r.col.FindOne(ctx, query, nil)
	if err = decodeable.Decode(token); err != nil {
		return nil, err
	}

	return token, nil
}

// GetTokensByOwner retrieves tokens for a specific owner based on the provided search parameters.
//
// Parameters:
// - ctx: the context in which the function is executed
// - owner: the unique identifier of the owner
// - asp: the search pagination parameters
//
// Returns:
// - []tokenmodels.AuthToken: a list of authentication tokens
// - error: an error if the operation fails
func (r *Repo) GetTokensByOwner(ctx context.Context, owner string, asp querymongo.ApiSearchPagination) ([]tokenmodels.AuthToken, error) {
	query := bson.M{
		"deleted_at": nil,
	}

	for key, value := range asp.Filters {
		if _, ok := searchableQueries[key]; !ok && !strings.Contains(key, "payload.") {
			continue
		}

		query[key] = value
	}

	query["owner"] = owner
	queryOptions := options.FindOptions{}
	if asp.Pagination != nil {
		queryOptions.Projection = querymongo.MergeDefaultProjection(asp.Pagination)
	}

	querymongo.SetMongoPagination(query, asp.Sort, asp.Pagination, &queryOptions)
	tokens := []tokenmodels.AuthToken{}

	cursor, err := r.col.Find(ctx, query, &queryOptions)
	if err != nil {
		return nil, err
	}

	err = cursor.All(ctx, &tokens)
	return tokens, err
}

// CountManyByOwner counts the number of documents in the collection based on the owner ID and filters.
//
// Parameters:
// - ctx: the context in which the function is executed
// - ownerID: the ID of the owner
// - filters: the filters to apply to the query
// Return type:
// - int64: the count of documents
// - error: an error if the operation fails
func (r *Repo) CountManyByOwner(ctx context.Context, ownerID string, filters bson.M) (int64, error) {
	if ownerID != "" {
		filters["owner_id"] = ownerID
	}
	filters["deleted_at"] = nil

	return r.col.CountDocuments(ctx, filters)
}

// GetPagination retrieves pagination information based on the provided owner ID, filters, URL, and elements.
//
// Parameters:
// - ctx: the context in which the function is executed
// - ownerID: the ID of the owner
// - filters: the filters to apply to the query
// - aspUrl: the URL for pagination
// - elements: a list of authentication tokens
// Return type:
// - querymongo.Pagination: the pagination information
func (r *Repo) GetPagination(ctx context.Context, ownerID string, filters bson.M, aspUrl *url.URL, elements []tokenmodels.AuthToken) querymongo.Pagination {
	if len(elements) == 0 {
		newURL, err := url.Parse(
			fmt.Sprintf(
				"%s://%s:%s",
				utils.GetEnv(utils.EnvPantahubScheme),
				utils.GetEnv(utils.EnvPantahubHost),
				utils.GetEnv(utils.EnvPantahubPort),
			),
		)
		if err != nil {
			newURL = aspUrl
		}
		newURL.Path = aspUrl.Path
		return querymongo.Pagination{
			Total:       0,
			Next:        "",
			Prev:        "",
			ResourceURL: newURL.String(),
		}
	}

	lastElement := len(elements)
	lastIndex := 0

	if lastElement > 0 {
		lastIndex = lastElement - 1
	}

	query := bson.M{}

	for key, value := range filters {
		if _, ok := searchableQueries[key]; !ok {
			continue
		}

		query[key] = value
	}

	total, err := r.CountManyByOwner(ctx, ownerID, query)
	if err != nil {
		total = int64(lastElement)
	}
	return querymongo.GetPaginationWithLink(*aspUrl, total, &elements[lastIndex], &elements[0])
}
