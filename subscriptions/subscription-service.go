// Copyright 2020  Pantacor Ltd.
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
package subscriptions

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
	"gopkg.in/mgo.v2/bson"
)

// SubscriptionPage pagination for subscription
type SubscriptionPage struct {
	Start int
	Page  int
	Size  int
	Subs  []Subscription
}

// SubscriptionService Interface offers primitives for loading, listing,
// saving and deleting of subscriptions.
type SubscriptionService interface {

	// Delete Subscription
	Delete(pctx context.Context, sub Subscription) error

	New(
		pctx context.Context,
		Subject utils.Prn,
		Issuer utils.Prn,
		Type utils.Prn,
		schema map[string]interface{}) (Subscription, error)

	// Check if user prn is in admins set for this instance
	IsAdmin(user utils.Prn) bool

	// Load subscription by ID
	Load(pctx context.Context, ID string) (Subscription, error)

	// Load subscription by ID
	LoadBySubject(pctx context.Context, subject utils.Prn) (Subscription, error)

	// Load subscription by ID
	GetDefaultSubscription(subject utils.Prn) Subscription

	// List subscription by owning "subject"
	List(pctx context.Context, Subject utils.Prn, start, page int) (SubscriptionPage, error)

	// Save subscription
	Save(pctx context.Context, sub Subscription) error

	// Now time
	Now() time.Time
}

type subscriptionService struct {
	mongoClient *mongo.Client `json:"-" bson:"-"`
	servicePrn  utils.Prn
	admins      []utils.Prn
	types       map[utils.Prn]interface{}
}

var (
	defaultSubscriptionID = bson.NewObjectId().Hex()
	defaultSubscription   = SubscriptionMgo{
		ID:         defaultSubscriptionID,
		Prn:        utils.Prn("prn::subscriptions:/" + defaultSubscriptionID),
		Issuer:     utils.Prn("prn:pantahub.com:auth:/admin"),
		Type:       SubscriptionTypeFree,
		Attributes: SubscriptionProperties[SubscriptionTypeFree].(map[string]interface{}),
	}
)

// New createsa a new Subscription. If subType is a known subscription
// type PRN, we will use the properties savesd for that sub type instead
// of the attributes provided as argument to this function.
func (i subscriptionService) New(
	parentCtx context.Context,
	subject utils.Prn,
	issuer utils.Prn,
	subType utils.Prn,
	attributes map[string]interface{}) (Subscription, error) {

	// create subscription object
	s := SubscriptionMgo{}
	s.ID = bson.NewObjectId().Hex()
	s.Prn = utils.Prn("prn::subscriptions:/" + s.ID)
	s.service = i
	s.Subject = subject
	s.Service = i.servicePrn
	s.Issuer = issuer
	s.Type = subType
	s.LastModified = i.Now()
	s.TimeCreated = s.LastModified

	// look up attributes to see if we have some.
	plans := strings.Split(string(s.Type), ":/")
	attrs, ok := SubscriptionProperties[utils.Prn(plans[0])]
	if !ok {
		return nil, errors.New("No such subscription plan available: " + string(s.Type))
	}

	if attrs != nil {
		s.Attributes = attrs.(map[string]interface{})
	}

	// all custom overwrites
	for k, v := range s.Attributes {
		s.Attributes[k] = v
	}

	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	_, err := collection.InsertOne(
		ctx,
		s,
	)
	if err != nil {
		return nil, err
	}

	// initialize original from original values
	return s, nil
}

func (i subscriptionService) IsAdmin(user utils.Prn) bool {
	for _, v := range i.admins {
		if v == user {
			return true
		}
	}
	return false
}

func (i subscriptionService) Load(pctx context.Context, ID string) (Subscription, error) {
	s := SubscriptionMgo{}
	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, bson.M{
		"_id": ID,
	}).Decode(&s)
	if err != nil {
		return nil, err
	}

	s.service = i
	return &s, nil
}

func (i subscriptionService) LoadBySubject(pctx context.Context, subject utils.Prn) (Subscription, error) {
	s := SubscriptionMgo{}
	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, bson.M{
		"subject": subject,
	}).Decode(&s)
	if err != nil {
		return nil, err
	}

	s.service = i
	return &s, nil
}

func (i subscriptionService) GetDefaultSubscription(subject utils.Prn) Subscription {
	sub := defaultSubscription
	sub.service = i
	defaultSubscription.LastModified = i.Now()
	defaultSubscription.TimeCreated = defaultSubscription.LastModified
	defaultSubscription.Subject = subject
	return sub
}

func (i subscriptionService) List(
	pctx context.Context,
	subject utils.Prn,
	start, page int) (SubscriptionPage, error) {

	resultPage := SubscriptionPage{
		Start: start,
		Page:  page,
	}

	subs := []SubscriptionMgo{}

	query := bson.M{}
	if subject != "" {
		query["subject"] = subject
	}
	if i.servicePrn != "" {
		query["service"] = i.servicePrn
	}

	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	findOptions := options.Find()
	if page >= 0 {
		findOptions.SetLimit(int64(page))
	}
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, query, findOptions)
	if err != nil {
		return resultPage, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := SubscriptionMgo{}
		err := cur.Decode(&result)
		if err != nil {
			return resultPage, err
		}
		subs = append(subs, result)
	}

	ctx, cancel = context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	count, err := collection.CountDocuments(ctx, query)
	if err != nil {
		return resultPage, err
	}
	resultPage.Size = int(count)
	resultPage.Page = page

	resultPage.Subs = make([]Subscription, len(subs))
	for j, v := range subs {
		v.service = i
		resultPage.Subs[j] = v
	}
	return resultPage, nil
}

func (i subscriptionService) Delete(pctx context.Context, sub Subscription) error {
	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	_, err := collection.DeleteOne(ctx, bson.M{
		"_id": sub.GetID(),
	})

	if err != nil {
		return err
	}
	return nil
}

func (i subscriptionService) Save(pctx context.Context, sub Subscription) error {

	s, ok := sub.(SubscriptionMgo)

	if !ok {
		return errors.New("wrong Subscription Type Passed to service")
	}

	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sub.GetID()},
		bson.M{"$set": s},
	)
	if err != nil {
		return err
	}
	return nil
}

func (i subscriptionService) Now() time.Time {
	return time.Now()
}

func (i subscriptionService) ensureIndices() error {
	collection := i.mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "service", Value: bsonx.Int32(1)},
			{Key: "subject", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for subscription: " + err.Error())
		return nil
	}

	return err
}

// NewService creates a new mongoClient backed subscription service
// Will use the default DB configured in mongoClient provided as arg.
func NewService(
	mongoClient *mongo.Client,
	servicePrn utils.Prn,
	admins []utils.Prn,
	typeDefs map[utils.Prn]interface{}) SubscriptionService {

	sub := new(subscriptionService)
	sub.mongoClient = mongoClient
	sub.servicePrn = servicePrn
	sub.admins = admins
	sub.types = typeDefs

	sub.ensureIndices()
	return sub
}
