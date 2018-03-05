// Package subscriptions offers simple subscription REST API to issue subscriptions
// for services. In this file we define the SubscriptionService interface and mongo
// backed implementation.
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//
package subscriptions

import (
	"errors"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

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
	Delete(sub Subscription) error

	New(Subject utils.Prn,
		Issuer utils.Prn,
		Type utils.Prn,
		schema map[string]interface{}) (Subscription, error)

	// Load subscription by ID
	Load(ID string) (Subscription, error)

	// Load subscription by ID
	LoadBySubject(subject utils.Prn) (Subscription, error)

	// Load subscription by ID
	GetDefaultSubscription(subject utils.Prn) Subscription

	// List subscription by owning "subject"
	List(Subject utils.Prn, start, page int) (SubscriptionPage, error)

	// Save subscription
	Save(sub Subscription) error

	// Now time
	Now() time.Time
}

type subscriptionService struct {
	mgoSession *mgo.Session `json:"-" bson:"-"`
	servicePrn utils.Prn
	admins     []utils.Prn
	types      map[utils.Prn]interface{}
}

var (
	defaultSubscriptionID = bson.NewObjectId().Hex()
	defaultSubscription   = SubscriptionMgo{
		ID:         defaultSubscriptionID,
		Prn:        utils.Prn("prn::subscriptions:/" + defaultSubscriptionID),
		Issuer:     utils.Prn("prn::auth:/admin"),
		Type:       SubscriptionTypeFree,
		Attributes: SubscriptionProperties[SubscriptionTypeFree].(map[string]interface{}),
	}
)

// New createsa a new Subscription. If subType is a known subscription
// type PRN, we will use the properties savesd for that sub type instead
// of the attributes provided as argument to this function.
func (i subscriptionService) New(subject utils.Prn,
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
	attrs, ok := SubscriptionProperties[s.Type]
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

	err := i.mgoSession.DB("").C(collectionSubscription).Insert(s)
	if err != nil {
		return nil, err
	}

	// initialize original from original values
	return s, nil
}

func (i subscriptionService) Load(ID string) (Subscription, error) {
	s := SubscriptionMgo{}
	err := i.mgoSession.DB("").C(collectionSubscription).FindId(ID).One(&s)
	if err != nil {
		return nil, err
	}

	s.service = i
	return &s, nil
}

func (i subscriptionService) LoadBySubject(subject utils.Prn) (Subscription, error) {
	s := SubscriptionMgo{}
	err := i.mgoSession.DB("").C(collectionSubscription).Find(bson.M{"subject": subject}).One(&s)
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

func (i subscriptionService) List(subject utils.Prn,
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

	mgoQuery := i.mgoSession.DB("").C(collectionSubscription).Find(query).Skip(start)

	count, err := mgoQuery.Count()
	if err != nil {
		return resultPage, err
	}
	resultPage.Size = count

	if page >= 0 {
		mgoQuery = mgoQuery.Limit(page)
	}
	resultPage.Page = page

	err = mgoQuery.All(&subs)
	if err != nil {
		return resultPage, err
	}

	resultPage.Subs = make([]Subscription, len(subs))
	for j, v := range subs {
		v.service = i
		resultPage.Subs[j] = v
	}
	return resultPage, nil
}

func (i subscriptionService) Delete(sub Subscription) error {
	err := i.mgoSession.
		DB("").C(collectionSubscription).
		RemoveId(sub.GetID())

	if err != nil {
		return err
	}
	return nil
}

func (i subscriptionService) Save(sub Subscription) error {

	s, ok := sub.(SubscriptionMgo)

	if !ok {
		return errors.New("Wrong Subscription Type Passed to service")
	}

	err := i.mgoSession.
		DB("").C(collectionSubscription).
		UpdateId(sub.GetID(), &s)

	if err != nil {
		return err
	}
	return nil
}

func (i subscriptionService) Now() time.Time {
	return time.Now()
}

func (i subscriptionService) ensureIndices() error {
	err := i.mgoSession.DB("").C(collectionSubscription).EnsureIndex(
		mgo.Index{
			Key:    []string{"service", "subject"},
			Unique: true,
		},
	)

	return err
}

// New creates a new mgo backed subscription service
// Will use the default DB configured in mgo.Sessino provided as arg.
func NewService(session *mgo.Session,
	servicePrn utils.Prn, admins []utils.Prn,
	typeDefs map[utils.Prn]interface{}) SubscriptionService {

	sub := new(subscriptionService)
	sub.mgoSession = session
	sub.servicePrn = servicePrn
	sub.admins = admins
	sub.types = typeDefs

	sub.ensureIndices()
	return sub
}
