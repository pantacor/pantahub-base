//
// Package subscriptions offers simple subscription REST API to issue subscriptions
// for services. In this file we define the SubscriptionService interface and mongo
// backed implementation.
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//
package subscriptions

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/mongodb/mongo-go-driver/mongo"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

var (
	mongoClient *mongo.Client
)

func setup(t *testing.T) {
	var err error

	mongoClient, err := utils.GetMongoClient()

	if err != nil {
		log.Println("error initiating mongoClient " + err.Error())
		os.Exit(1)
	}

	// Count all to check if we can talk to DB
	collection := mongoClient.Database(utils.MongoDb).Collection(collectionSubscription)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.CountDocuments(ctx,
		bson.M{},
	)
	if err != nil {
		t.Errorf("Fail to access collection '%s' test setup. error: %s",
			collectionSubscription, err.Error())
		t.Fail()
		return
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = mongoClient.Database(utils.MongoDb).Collection(collectionSubscription).Drop(ctx)
	if err != nil {
		t.Logf("Warning: failed to drop collection '%s' in test setup. error: %s",
			collectionSubscription, err.Error())
		return
	}
}

func newTestService() SubscriptionService {
	wrappedService := NewService(mongoClient, utils.Prn("prn:pantahub.com:base:/"),
		[]utils.Prn{
			"prn::auth:/admin",
			"prn::auth:/admin2",
		},
		SubscriptionProperties,
	)

	svc := wrappedService.(*subscriptionService)

	wrapper := subscriptionServiceTest{
		subscriptionService: *svc,
		now:                 time.Now(),
	}
	return wrapper
}

func testNewSubscription(t *testing.T) {
	sService := newTestService()

	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCancelled, nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	_, err = sService.Load(sub.GetID())

	if err != nil {
		t.Errorf("new subscriptions must be found in db: %s", err.Error())
		t.Fail()
	}

	sub1, err := sService.Load(sub.GetID())

	if err != nil {
		t.Errorf("subscriptions must not fail to load after they got saved to DB")
		t.Fail()
	}

	if sub1.GetIssuer() != sub.GetIssuer() {
		t.Errorf("subscription loaded and saved must have same issuer")
		t.Fail()
	}
	if sub1.GetSubject() != sub.GetSubject() {
		t.Errorf("subscription loaded and saved must have same subject")
		t.Fail()
	}
	if sub1.GetService() != sub.GetService() {
		t.Errorf("subscription loaded and saved must have same subject")
		t.Fail()
	}
	if sub1.GetPlan() != sub.GetPlan() {
		t.Errorf("subscription loaded and saved must have same type")
		t.Fail()
	}
	if sub1.GetPrn() != sub.GetPrn() {
		t.Errorf("subscription loaded and saved must have same Prn")
		t.Fail()
	}
	prn := sub1.GetPrn()
	_, err = prn.GetInfo()
	if err != nil {
		t.Errorf("subscription loaded and saved must have valid Prn (parse error: %s)", err.Error())
		t.Fail()
	}
}

func testNewSubscriptionWithDefaults(t *testing.T) {
	sService := newTestService()

	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"),
		SubscriptionTypeFree,
		nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	sub1, err := sService.Load(sub.GetID())
	if err != nil {
		t.Errorf("Subscriptions must not fail to load after they got saved to DB")
		t.Fail()
		return
	}
	if !sub1.HasProperty("BANDWIDTH") {
		t.Errorf("Subscription must have network property")
		t.Fail()
		return
	}

	m := SubscriptionProperties[SubscriptionTypeFree].(map[string]interface{})

	if sub1.GetProperty("BANDWIDTH") != m["BANDWIDTH"] {
		t.Errorf("Subscription must have network property")
		t.Fail()
		return
	}
}

func testNewCustomSubscription(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	_, err = sService.Load(sub.GetID())

	if err != nil {
		t.Errorf("new subscriptions must be found in db: %s", err.Error())
		t.Fail()
	}
}

func testNewTimeModified(t *testing.T) {
	timeStart := time.Now()
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	tm := sub.GetTimeModified()

	if timeStart.After(tm) {
		t.Errorf("time modified not set on subscription creation: %s", tm.String())
		t.Fail()
		return
	}

	_, err = sService.Load(sub.GetID())

	if err != nil {
		t.Errorf("new subscriptions must be found in db: %s", err.Error())
		t.Fail()
	}

	if !tm.Equal(sub.GetTimeModified()) {
		t.Errorf("time modified saved after load is different from time before save: %s != %s",
			tm.String(), sub.GetTimeModified().String())
		t.Fail()
		return
	}
}

func testNewCustomOverwriteSubscription(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	sub1, err := sService.Load(sub.GetID())

	if err != nil {
		t.Errorf("new subscriptions must be found in db: %s", err.Error())
		t.Fail()
	}

	err = sub1.UpdatePlan(utils.Prn("prn::auth:/admin1"), SubscriptionTypeFree, map[string]interface{}{
		"ALL/storage": "4GiB",
		"ALL/network": "2GiB",
	})

	if err != nil {
		t.Errorf("update plan must not fail: %s", err.Error())
		t.Fail()
	}

	sub1, err = sService.Load(sub.GetID())

	if err != nil {
		t.Errorf("loading updated subsription must not fail: %s", err.Error())
		t.Fail()
	}

	if sub1.GetProperty("ALL/storage").(string) != "4GiB" {
		t.Errorf("overwrite does not work. storage should be 4GiB, but is %s", sub1.GetProperty("ALL/storage"))
		t.Fail()
	}
}

func testDeleteSubscription(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	err = sService.Delete(sub)

	if err != nil {
		t.Errorf("delete subscription failed: %s", err.Error())
		t.Fail()
	}

	_, err = sService.Load(sub.GetID())

	if err == nil {
		t.Errorf("deleted subscription must not be loadable")
		t.Fail()
	}
}

func testSaveDeletedSubscription(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, nil)

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	err = sService.Delete(sub)

	if err != nil {
		t.Errorf("delete subscription failed: %s", err.Error())
		t.Fail()
	}

	err = sService.Save(sub)

	if err == nil {
		t.Errorf("deleted subscriptions must fail to save")
		t.Fail()
	}
}

func testCancelSubscription(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, bson.M{})

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	err = sub.Cancel(utils.Prn("prn::auth:/admin1"))
	if err != nil {
		t.Errorf("error cancelling subscriptions: %s", err.Error())
		t.Fail()
		return
	}

	// until load we are not locked (we dont use pointer ref in interface)
	if sub.IsCancelled() {
		t.Errorf("locked subscription cancelled before reload")
		t.Fail()
	}

	sub, err = sService.Load(sub.GetID())
	if err != nil {
		t.Errorf("loading subscription must not fail: %s", err.Error())
		t.Fail()
		return
	}

	if !sub.IsCancelled() {
		t.Errorf("reloaded cancelled subscription not cancelled")
		t.Fail()
		return
	}
}

func testLockSubscription(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, bson.M{})

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	err = sub.Lock(utils.Prn("prn::auth:/admin1"))
	if err != nil {
		t.Errorf("error locking subscriptions: %s", err.Error())
		t.Fail()
		return
	}

	// until load we are not locked (we dont use pointer ref in interface)
	if sub.IsLocked() {
		t.Errorf("locked subscription locked before reload")
		t.Fail()
	}

	sub, err = sService.Load(sub.GetID())
	if err != nil {
		t.Errorf("loading subscription must not fail: %s", err.Error())
		t.Fail()
		return
	}

	if !sub.IsLocked() {
		t.Errorf("reloaded locked subscription not locked")
		t.Fail()
		return
	}
}

func testHistory(t *testing.T) {
	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, bson.M{})

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	err = sub.Lock(utils.Prn("prn::auth:/admin1"))
	if err != nil {
		t.Errorf("error locking subscriptions: %s", err.Error())
		t.Fail()
		return
	}

	sub, err = sService.Load(sub.GetID())
	if err != nil {
		t.Errorf("loading subscription must not fail: %s", err.Error())
		t.Fail()
		return
	}

	err = sub.UpdatePlan(utils.Prn("prn::auth:/admin1"), SubscriptionTypeVIP, nil)
	if err != nil {
		t.Errorf("updating plan to VIP must not fail: %s", err.Error())
		t.Fail()
		return
	}

	sub, err = sService.Load(sub.GetID())
	if err != nil {
		t.Errorf("loading subscription must not fail: %s", err.Error())
		t.Fail()
		return
	}

	h := sub.GetHistory()

	if len(h) != 2 {
		t.Errorf("history length must be 2 not %d", len(h))
		t.Fail()
		return
	}

	if h[0].GetPlan() != SubscriptionTypeCustom {
		t.Errorf("history entry 0 must be type %s, not %s", SubscriptionTypeCustom, h[0].GetPlan())
		t.Fail()
		return
	}

	if !h[1].IsLocked() {
		t.Errorf("history entry 1 must be locked")
		t.Fail()
		return
	}
}

func testList(t *testing.T) {
	sService := newTestService()
	_, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, bson.M{})

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	subPage, err := sService.List(utils.Prn("prn::auth:/user1"), 0, -1)
	if subPage.Size != 1 {
		t.Errorf("subscription list must be 1, not %d", subPage.Size)
		t.Fail()
		return
	}
}

func TestNew(t *testing.T) {
	setup(t)
	if t.Failed() {
		return
	}
	t.Run("new-subscription", testNewSubscription)

	setup(t)
	if t.Failed() {
		return
	}
	t.Run("new-subscription-with-defaults", testNewSubscriptionWithDefaults)

	setup(t)
	if t.Failed() {
		return
	}
	t.Run("new-custom-subscription", testNewCustomSubscription)

	setup(t)
	if t.Failed() {
		return
	}
	t.Run("new-custom-overwrite-subscription", testNewCustomOverwriteSubscription)

	setup(t)
	if t.Failed() {
		return
	}
	t.Run("new-time-modified", testNewTimeModified)
}

func TestChange(t *testing.T) {
	setup(t)
	if t.Failed() {
		return
	}
	t.Run("lock-subscription", testLockSubscription)

	setup(t)
	if t.Failed() {
		return
	}
	t.Run("cancel-subscription", testCancelSubscription)
}

func TestDelete(t *testing.T) {
	setup(t)
	if t.Failed() {
		return
	}
	t.Run("delete-subscription", testDeleteSubscription)

	setup(t)
	if t.Failed() {
		return
	}
	t.Run("save-deleted-subscription", testSaveDeletedSubscription)
}

func TestHistory(t *testing.T) {
	setup(t)
	if t.Failed() {
		return
	}
	t.Run("subscription-history", testHistory)
}

func TestList(t *testing.T) {
	setup(t)
	if t.Failed() {
		return
	}
	t.Run("subscription-list", testList)
}

type subscriptionServiceTest struct {
	subscriptionService
	now time.Time
}

func (i subscriptionServiceTest) Now() time.Time {
	return i.now
}

func TestPeriod(t *testing.T) {
	setup(t)

	sService := newTestService()
	sub, err := sService.New(utils.Prn("prn::auth:/user1"),
		utils.Prn("prn::auth:/admin1"), SubscriptionTypeCustom, bson.M{})

	if err != nil {
		t.Errorf("error creating subscription: %s", err.Error())
		t.Fail()
		return
	}

	now := sService.Now()
	nowMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	start := sub.GetPeriodStart()

	if !nowMonth.Equal(start) {
		t.Errorf("period start does not equal nowMonth: %s != %s ", start.String(), nowMonth.String())
		t.Fail()
	}

	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	end := sub.GetPeriodEnd()
	if !nextMonth.Equal(end) {
		t.Errorf("period start does not equal nowMonth: %s != %s ", start.String(), nowMonth.String())
		t.Fail()
	}

	progress := sub.GetPeriodProgression()
	if progress < 0 || progress > 1.0 {
		t.Errorf("period progress must be greater or equal than zero or smaller or equal than 1.0, not %f ", progress)
		t.Fail()
	}
}
