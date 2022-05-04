//
// Package subscriptions offers simple subscription REST API to issue subscriptions
// for services
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//
package subscriptions

import (
	"errors"
	"math"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	collectionSubscription = "pantahub_subscriptions"

	// SubscriptionTypeCustom custom subscription
	SubscriptionTypeCustom = utils.Prn("prn::subscriptions:CUSTOM")

	// SubscriptionTypeCancelled canceled subscription
	SubscriptionTypeCancelled = utils.Prn("prn::subscriptions:CANCELLED")

	// SubscriptionTypeLocked locked subscription
	SubscriptionTypeLocked = utils.Prn("prn::subscriptions:LOCKED")

	// SubscriptionTypePrefix prefix for all subscription
	SubscriptionTypePrefix = utils.Prn("prn::subscriptions:")

	// SubscriptionTypeFree free subscription
	SubscriptionTypeFree = utils.Prn(SubscriptionTypePrefix + "FREE")

	// SubscriptionTypeVIP vip subscription
	SubscriptionTypeVIP = utils.Prn(SubscriptionTypePrefix + "VIP")
)

// Subscription define a subscription interface
type Subscription interface {
	GetID() string
	GetPrn() utils.Prn
	GetPlan() utils.Prn
	GetIssuer() utils.Prn
	GetSubject() utils.Prn
	GetService() utils.Prn
	GetTimeModified() time.Time
	GetTimeCreated() time.Time

	GetPeriodStart() time.Time
	GetPeriodEnd() time.Time
	GetPeriodProgression() float64

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

// SubscriptionMgo define Subscription mongo payload
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

	// the time this subscription was modified.
	TimeCreated time.Time `json:"time-created" bson:"time-created"`

	// History log in cronological order (earliest first) . Max history is not implemented rightnow..
	History []SubscriptionMgo `json:"history,omitempty" bson:"history,omitempty"`

	Attributes map[string]interface{} `json:"attr,omitempty" bson:"attr,omitempty"`
}

var (
	// SubscriptionProperties define the subscriptions capabilities
	SubscriptionProperties = map[utils.Prn]interface{}{
		SubscriptionTypeFree: map[string]interface{}{
			"OBJECTS":   "2GiB",
			"BANDWIDTH": "2GiB",
			"DEVICES":   "25",
		},
		SubscriptionTypeVIP: map[string]interface{}{
			"OBJECTS":   "20GiB",
			"BANDWIDTH": "10GiB",
			"DEVICES":   "100",
		},
		SubscriptionTypeLocked:    nil,
		SubscriptionTypeCancelled: nil,
		SubscriptionTypeCustom: map[string]interface{}{
			"OBJECTS":   "0GiB",
			"BANDWIDTH": "0GiB",
			"DEVICES":   "0",
		},
	}
)

// GetID get subscription ID
func (i SubscriptionMgo) GetID() string {
	return i.ID
}

// GetIssuer get subscription issuer
func (i SubscriptionMgo) GetIssuer() utils.Prn {
	return i.Issuer
}

// GetPlan get subscription type
func (i SubscriptionMgo) GetPlan() utils.Prn {
	return i.Type
}

// GetPrn get subscription PRN
func (i SubscriptionMgo) GetPrn() utils.Prn {
	return i.Prn
}

// GetSubject get subscription subject
func (i SubscriptionMgo) GetSubject() utils.Prn {
	return i.Subject
}

// GetService get subscription service
func (i SubscriptionMgo) GetService() utils.Prn {
	return i.Service
}

// HasProperty check if a subscription has a specific property
func (i SubscriptionMgo) HasProperty(key string) bool {
	_, ok := i.Attributes[key]
	return ok
}

// GetProperty get a subscription property
func (i SubscriptionMgo) GetProperty(key string) interface{} {
	return i.Attributes[key]
}

// GetHistory get the history of a subscription
func (i SubscriptionMgo) GetHistory() []Subscription {
	subs := make([]Subscription, len(i.History))
	for k := range i.History {
		s := i.History[k]
		subs[k] = s
	}
	return subs
}

// GetTimeModified last time the subscription was modified
func (i SubscriptionMgo) GetTimeModified() time.Time {
	return i.LastModified
}

// GetTimeCreated Get the time when the subscription was created
func (i SubscriptionMgo) GetTimeCreated() time.Time {
	return i.TimeCreated
}

// GetPeriodStart get when the current period of the subscription started
func (i SubscriptionMgo) GetPeriodStart() time.Time {
	now := i.service.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// GetPeriodEnd get when the current period end
func (i SubscriptionMgo) GetPeriodEnd() time.Time {
	now := i.service.Now().UTC()
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

// GetPeriodProgression get the progression of the current period
func (i SubscriptionMgo) GetPeriodProgression() float64 {
	start := i.GetPeriodStart()
	end := i.GetPeriodEnd()
	now := i.service.Now()
	periodLenSec := end.Sub(start)
	periodIn := now.Sub(start)
	return math.Abs(float64(periodIn) / float64(periodLenSec))
}

// IsCancelled check if the subscription is cancelled
func (i SubscriptionMgo) IsCancelled() bool {
	return i.Type == SubscriptionTypeCancelled
}

// IsLocked check if the subscription is locked
func (i SubscriptionMgo) IsLocked() bool {
	return i.Type == SubscriptionTypeLocked
}

// UpdatePlan udpdate a plan with new configuration and saved the previous as history
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

	// look up attributes to see if we have some.
	subAttrs, ok := SubscriptionProperties[plan]
	if !ok {
		return errors.New("No such subscription plan available: " + string(plan))
	}

	if subAttrs != nil {
		i.Attributes = subAttrs.(map[string]interface{})
	}
	// all custom overwrites
	for k, v := range attrs {
		i.Attributes[k] = v
	}

	i.LastModified = i.service.Now()
	err := i.service.Save(i)

	return err
}

// Cancel cancel a subscription
func (i SubscriptionMgo) Cancel(issuer utils.Prn) error {
	err := i.UpdatePlan(issuer, SubscriptionTypeCancelled, i.Attributes)
	return err
}

// Lock lock a subscription
func (i SubscriptionMgo) Lock(issuer utils.Prn) error {
	err := i.UpdatePlan(issuer, SubscriptionTypeLocked, i.Attributes)
	return err
}
