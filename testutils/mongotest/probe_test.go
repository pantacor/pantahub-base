package mongotest_test

import (
	"context"
	"testing"

	"gitlab.com/pantacor/pantahub-base/testutils/mongotest"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Proves the container is a real replica set: utils.GetMongoClient applies
// majority read/write concern, which a standalone mongod rejects.
func TestContainerSatisfiesMajorityConcern(t *testing.T) {
	ctr := mongotest.Setup(t)
	t.Logf("mongo at %s:%s", ctr.Host, ctr.Port)

	client, err := utils.GetMongoClient()
	if err != nil {
		t.Fatalf("GetMongoClient: %v", err)
	}
	if err := client.Ping(context.Background(), nil); err != nil {
		t.Fatalf("ping: %v", err)
	}

	coll := client.Database("testdb").Collection("probe")
	if _, err := coll.InsertOne(context.Background(), map[string]any{"hello": "world"}); err != nil {
		t.Fatalf("majority-concern write failed (not a replica set?): %v", err)
	}
	var out map[string]any
	if err := coll.FindOne(context.Background(), map[string]any{"hello": "world"}).Decode(&out); err != nil {
		t.Fatalf("majority-concern read failed: %v", err)
	}
	t.Logf("round-tripped: %v", out)
}
