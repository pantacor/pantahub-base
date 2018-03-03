// Package subscriptions offers simple subscription REST API to issue subscriptions
// for services
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//
package subscriptions

import (
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	collectionSubscription = "pantabase_subscription"

	SubscriptionTypeCustom    = utils.Prn("prn::subscriptions:CUSTOM")
	SubscriptionTypeCancelled = utils.Prn("prn::subscriptions:CANCELLED")
	SubscriptionTypeLocked    = utils.Prn("prn::subscriptions:LOCKED")
	SubscriptionTypePrefix    = utils.Prn("prn::subscriptions:/")
	SubscriptionTypeFree      = utils.Prn(SubscriptionTypePrefix + "FREE")
	SubscriptionTypeVIP       = utils.Prn(SubscriptionTypePrefix + "/VIP")
)

type Subscription interface {
	GetID() string
	GetPrn() utils.Prn
	GetPlan() utils.Prn
	GetIssuer() utils.Prn
	GetSubject() utils.Prn
	GetService() utils.Prn
	GetTimeModified() time.Time

	HasProperty(key string) bool
	GetProperty(key string) interface{}

	IsLocked() bool
	IsCancelled() bool

	GetHistory() []Subscription

	// UpdatePlan changes plan for subscription to plan PRN. If not nil,
	// attrs will overload the defaults that come with plan.
	UpdatePlan(issuer utils.Prn, plan utils.Prn, attrs map[string]interface{}) error
	Cancel(issuer utils.Prn) error
	Lock(issuer utils.Prn) error
}

type SubscriptionMgo struct {
	service SubscriptionService

	// The ID for the subscription in mongo
	ID string `json:"id" bson:"_id"`

	// The Prn of the subscription
	Prn utils.Prn `json:"prn" bson:"prn"`

	// The Type of the subscription in PRN format
	Type utils.Prn `json:"type" bson:"type"`

	// the subject of a subscription (service consumer!)
	Subject utils.Prn `json:"subject" bson:"subject"`

	// the issuer of a subscription (service operator!)
	Issuer utils.Prn `json:"issuer" bson:"issuer"`

	// the service a subscription is valid for (e.g. prn::services:/pantahub-base)
	Service utils.Prn `json:"service" bson:"service"`

	// the time this subscription was modified.
	LastModified time.Time `json:"last-modified" bson:"last-modified"`

	// History log in cronological order (earliest first) . Max history is not implemented rightnow..
	History []SubscriptionMgo `json:"history,omitempty", bson:"history,omitempty"`

	Attributes map[string]interface{} `json:"attr,omitempty" bson:"attr,omitempty"`
}

var (
	SubscriptionProperties = map[utils.Prn]interface{}{
		SubscriptionTypeFree: map[string]interface{}{
			"storage": "2GiB",
			"network": "2GiB",
		},
		SubscriptionTypeVIP: map[string]interface{}{
			"storage": "20GiB",
			"network": "10GiB",
		},
	}
)

func (i SubscriptionMgo) GetID() string {
	return i.ID
}
func (i SubscriptionMgo) GetIssuer() utils.Prn {
	return i.Issuer
}
func (i SubscriptionMgo) GetPlan() utils.Prn {
	return i.Type
}
func (i SubscriptionMgo) GetPrn() utils.Prn {
	return i.Prn
}
func (i SubscriptionMgo) GetSubject() utils.Prn {
	return i.Subject
}
func (i SubscriptionMgo) GetService() utils.Prn {
	return i.Service
}
func (i SubscriptionMgo) HasProperty(key string) bool {
	_, ok := i.Attributes[key]
	return ok
}
func (i SubscriptionMgo) GetProperty(key string) interface{} {
	return i.Attributes[key]
}

func (i SubscriptionMgo) GetHistory() []Subscription {
	subs := make([]Subscription, len(i.History))
	for k := range i.History {
		s := i.History[k]
		subs[k] = s
	}
	return subs
}

func (i SubscriptionMgo) GetTimeModified() time.Time {
	return i.LastModified
}
func (i SubscriptionMgo) IsCancelled() bool {
	return i.Type == SubscriptionTypeCancelled
}
func (i SubscriptionMgo) IsLocked() bool {
	return i.Type == SubscriptionTypeLocked
}

func (i SubscriptionMgo) UpdatePlan(issuer utils.Prn, plan utils.Prn, attrs map[string]interface{}) error {

	// create a clone where we can strip history history
	c := i
	// strip history of clone (avoid recursive storyage)
	c.History = nil
	// append clone to history list
	i.History = append(i.History, c)

	// change subscription now and save it later ...
	i.Issuer = issuer
	i.Type = plan

	if attrs == nil {
		i.Attributes = SubscriptionProperties[plan].(map[string]interface{})
	} else {
		i.Attributes = attrs
	}

	err := i.service.Save(i)

	return err
}

func (i SubscriptionMgo) Cancel(issuer utils.Prn) error {
	err := i.UpdatePlan(issuer, SubscriptionTypeCancelled, i.Attributes)
	return err
}

func (i SubscriptionMgo) Lock(issuer utils.Prn) error {
	err := i.UpdatePlan(issuer, SubscriptionTypeLocked, i.Attributes)
	return err
}
