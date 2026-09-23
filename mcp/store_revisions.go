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

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/trails/stateops"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// plansCollection holds revision plans until they are committed or
	// expire. A plan is the exact state commit_revision will post.
	plansCollection = "pantahub_mcp_revision_plans"

	// planTTL is how long a plan waits to be committed.
	planTTL = time.Hour

	trailsCollection = "pantahub_trails"
)

var (
	// errPlanGone covers a plan that never existed, expired, was committed
	// already, or belongs to somebody else.
	errPlanGone = errors.New("no such plan: it expired, was already committed, or was never made")

	// errRevisionMoved means the device got a revision after the plan was made.
	errRevisionMoved = errors.New("the device got a new revision since this plan was made")
)

// revisionPlan is one planned revision.
type revisionPlan struct {
	ID          string                 `bson:"_id"`
	Owner       string                 `bson:"owner"`
	Device      primitive.ObjectID     `bson:"device"`
	DeviceNick  string                 `bson:"device_nick"`
	BaseRev     int                    `bson:"base_rev"`
	State       map[string]interface{} `bson:"state"`
	Diff        stateops.Diff          `bson:"diff"`
	Warnings    []string               `bson:"warnings"`
	Description string                 `bson:"description"`
	ClientID    string                 `bson:"client_id,omitempty"`
	CreatedAt   time.Time              `bson:"created_at"`
	ExpiresAt   time.Time              `bson:"expires_at"`
	Used        bool                   `bson:"used,omitempty"`
}

// ensurePlanIndexes lets mongo drop plans nobody committed.
func (s *store) ensurePlanIndexes(ctx context.Context) error {
	_, err := s.collection(plansCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
	return err
}

func newPlanID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// savePlan stores plan and fills in its id and times. The expiry index is
// ensured here rather than at startup: plans are rare, the call is a no-op
// once the index exists, and the endpoint starts without the database.
func (s *store) savePlan(ctx context.Context, plan *revisionPlan) error {
	if err := s.ensurePlanIndexes(ctx); err != nil {
		log.Println("WARNING: mcp revision plans expiry index: " + err.Error())
	}

	id, err := newPlanID()
	if err != nil {
		return err
	}
	now := time.Now()
	plan.ID = id
	plan.CreatedAt = now
	plan.ExpiresAt = now.Add(planTTL)

	stored := *plan
	stored.State = utils.BsonQuoteMap(&plan.State)

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	_, err = s.collection(plansCollection).InsertOne(ctx, stored)
	return err
}

// claimPlan takes the owner's plan for committing, once: of two commits of
// one plan only one gets it.
func (s *store) claimPlan(ctx context.Context, owner, id string) (*revisionPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	plan := &revisionPlan{}
	err := s.collection(plansCollection).FindOneAndUpdate(ctx,
		bson.M{"_id": id, "owner": owner, "used": bson.M{"$ne": true}, "expires_at": bson.M{"$gt": time.Now()}},
		bson.M{"$set": bson.M{"used": true}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(plan)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errPlanGone
	}
	if err != nil {
		return nil, err
	}
	plan.State = utils.BsonUnquoteMap(&plan.State)
	return plan, nil
}

// releasePlan gives a claimed plan back after a commit that failed for a
// reason retrying may fix.
func (s *store) releasePlan(ctx context.Context, owner, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queryTimeout)
	defer cancel()
	_, _ = s.collection(plansCollection).UpdateOne(ctx,
		bson.M{"_id": id, "owner": owner},
		bson.M{"$set": bson.M{"used": false}})
}

// createRevision posts state as revision rev of the owner's device, through
// the same code POST /trails/:id/steps runs.
func (s *store) createRevision(ctx context.Context, owner string, deviceID primitive.ObjectID, rev int, state, meta map[string]interface{}, message string) (*trailmodels.Step, error) {
	trail := trailmodels.Trail{}
	lookup, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	err := s.collection(trailsCollection).FindOne(lookup,
		bson.M{"_id": deviceID, "owner": owner, "garbage": bson.M{"$ne": true}}).Decode(&trail)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}

	step, status, err := trails.Build(s.mongoClient).CreateStep(ctx, trail, trailmodels.Step{
		Rev:       rev,
		State:     state,
		Meta:      meta,
		CommitMsg: message,
	}, true)
	switch {
	case errors.Is(err, trails.ErrStepExists):
		return nil, errRevisionMoved
	case err != nil && (status < http.StatusInternalServerError || strings.Contains(err.Error(), "state_object")):
		// Bad requests, and a state naming objects the account does not
		// have, are worth telling; the rest is internal.
		return nil, invalid("the revision was refused: %s", err.Error())
	case err != nil:
		return nil, err
	}
	return step, nil
}
