package trailmodels

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestDownloadBucket(t *testing.T) {
	cases := []struct {
		downloaded, total, want int64
	}{
		{0, 0, 0}, {5, 0, 0}, {0, 100, 0}, {9, 100, 0}, {10, 100, 1}, {99, 100, 9}, {100, 100, 10},
	}
	for _, c := range cases {
		if got := DownloadBucket(c.downloaded, c.total); got != c.want {
			t.Errorf("DownloadBucket(%d,%d)=%d want %d", c.downloaded, c.total, got, c.want)
		}
	}
}

func TestSanitizeStepProgress(t *testing.T) {
	p := StepProgress{Logs: strings.Repeat("x", ProgressLogsFieldMax+10)}
	SanitizeStepProgress(&p)
	if len(p.Logs) != ProgressLogsFieldMax {
		t.Fatalf("logs not truncated: %d", len(p.Logs))
	}
	if got := TruncateProgressMsg(strings.Repeat("m", ProgressLogMsgMax+1)); len(got) != ProgressLogMsgMax {
		t.Fatalf("msg not truncated: %d", len(got))
	}
}

func TestNewProgressLogEntry(t *testing.T) {
	now := time.Now()
	p := StepProgress{Status: "DOWNLOADING", Progress: 42, StatusMsg: strings.Repeat("m", 300), Retries: 1}
	p.Downloads.Total.TotalSize = 1000
	p.Downloads.Total.TotalDownloaded = 420
	e := NewProgressLogEntry(p, ProgressLogSourceDevice, now)
	if e.Status != "DOWNLOADING" || e.Progress != 42 || e.Retries != 1 || e.Source != ProgressLogSourceDevice || !e.Time.Equal(now) {
		t.Fatalf("bad entry: %+v", e)
	}
	if len(e.StatusMsg) != ProgressLogMsgMax {
		t.Fatalf("msg not truncated in entry: %d", len(e.StatusMsg))
	}
	if e.Downloaded != 420 || e.TotalSize != 1000 {
		t.Fatalf("download totals not copied: %+v", e)
	}
	// no download info => no download fields
	e2 := NewProgressLogEntry(StepProgress{Status: "QUEUED"}, ProgressLogSourceDevice, now)
	if e2.Downloaded != 0 || e2.TotalSize != 0 {
		t.Fatalf("unexpected download fields: %+v", e2)
	}
}

func TestAppendProgressLogDedupAndCap(t *testing.T) {
	var log []ProgressLogEntry
	now := time.Now()

	log = AppendProgressLog(log, ProgressLogEntry{Time: now, Status: "NEW", StatusMsg: "step created"})
	log = AppendProgressLog(log, ProgressLogEntry{Time: now.Add(time.Second), Status: "NEW", StatusMsg: "step created"})
	if len(log) != 1 {
		t.Fatalf("repeat was appended: %d", len(log))
	}

	// download reports inside the same bucket are one line, crossing a bucket is a new line
	log = AppendProgressLog(log, ProgressLogEntry{Status: "DOWNLOADING", Downloaded: 10, TotalSize: 1000})
	log = AppendProgressLog(log, ProgressLogEntry{Status: "DOWNLOADING", Downloaded: 90, TotalSize: 1000})
	if len(log) != 2 {
		t.Fatalf("same bucket appended: %d", len(log))
	}
	log = AppendProgressLog(log, ProgressLogEntry{Status: "DOWNLOADING", Downloaded: 100, TotalSize: 1000})
	if len(log) != 3 {
		t.Fatalf("bucket change not appended: %d", len(log))
	}

	for i := 0; i < ProgressLogCap*2; i++ {
		log = AppendProgressLog(log, ProgressLogEntry{Status: "INPROGRESS", Progress: i})
	}
	if len(log) != ProgressLogCap {
		t.Fatalf("cap not enforced: %d", len(log))
	}
	if last := log[len(log)-1]; last.Progress != ProgressLogCap*2-1 {
		t.Fatalf("newest entry lost: %+v", last)
	}
}

// TestProgressUpdatePipelineMarshals checks the pipeline is a valid BSON
// document and that user data is wrapped in $literal so it is never evaluated.
func TestProgressUpdatePipelineMarshals(t *testing.T) {
	p := StepProgress{Status: "ERROR", StatusMsg: "$progress.status", Data: map[string]interface{}{"k": "$x"}}
	pipe := ProgressUpdatePipeline(p, ProgressLogSourceDevice, time.Now(), bson.M{"ispublic": true})
	if len(pipe) != 1 {
		t.Fatalf("expected one stage, got %d", len(pipe))
	}
	raw, err := bson.Marshal(pipe[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var stage bson.M
	if err := bson.Unmarshal(raw, &stage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	set := stage["$set"].(bson.M)
	if _, ok := set["progress"].(bson.M)["$literal"]; !ok {
		t.Fatalf("progress not wrapped in $literal: %v", set["progress"])
	}
	if _, ok := set["ispublic"].(bson.M)["$literal"]; !ok {
		t.Fatalf("extra field not wrapped in $literal: %v", set["ispublic"])
	}
	if _, ok := set[ProgressLogField]; !ok {
		t.Fatalf("no progress-log in $set")
	}
}

// TestProgressUpdatePipelineMongo runs the pipeline against a real server.
// Set PH_TEST_MONGO_URI (a primary, e.g.
// mongodb://admin:admin@localhost:30003/?directConnection=true&authSource=admin)
// to enable it; it is skipped otherwise.
func TestProgressUpdatePipelineMongo(t *testing.T) {
	uri := os.Getenv("PH_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("PH_TEST_MONGO_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("cannot connect: %v", err)
	}
	defer client.Disconnect(ctx)
	coll := client.Database("ph_progresslog_test").Collection("steps")
	defer coll.Drop(ctx)

	id := "test-" + time.Now().Format("150405.000")
	step := Step{ID: id, Owner: "prn:::accounts:/o", Device: "prn:::devices:/d", StepProgress: StepProgress{Status: "NEW"}}
	step.ProgressLog = []ProgressLogEntry{{Time: time.Now(), Source: ProgressLogSourceOwner, Status: "NEW", StatusMsg: "step created"}}
	if _, err := coll.InsertOne(ctx, step); err != nil {
		t.Fatalf("insert: %v", err)
	}

	apply := func(p StepProgress) Step {
		t.Helper()
		res, err := coll.UpdateOne(ctx, bson.M{"_id": id},
			ProgressUpdatePipeline(p, ProgressLogSourceDevice, time.Now(), bson.M{"ispublic": false, "progress-time": time.Now()}))
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if res.MatchedCount != 1 {
			t.Fatalf("no match")
		}
		var got Step
		if err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&got); err != nil {
			t.Fatalf("find: %v", err)
		}
		return got
	}

	got := apply(StepProgress{Status: "QUEUED", StatusMsg: "$progress.status", Data: map[string]interface{}{"k": "$v"}})
	if got.StepProgress.Status != "QUEUED" || got.StepProgress.StatusMsg != "$progress.status" {
		t.Fatalf("progress not stored literally: %+v", got.StepProgress)
	}
	if len(got.ProgressLog) != 2 || got.ProgressLog[1].Status != "QUEUED" || got.ProgressLog[1].Source != ProgressLogSourceDevice {
		t.Fatalf("QUEUED line missing: %+v", got.ProgressLog)
	}

	// identical report: progress replaced, no new line
	got = apply(StepProgress{Status: "QUEUED", StatusMsg: "$progress.status"})
	if len(got.ProgressLog) != 2 {
		t.Fatalf("repeat appended: %+v", got.ProgressLog)
	}

	// downloading: lines only when the bucket changes
	dl := func(done int64) StepProgress {
		p := StepProgress{Status: "DOWNLOADING", StatusMsg: "downloading"}
		p.Downloads.Total.TotalSize = 1000
		p.Downloads.Total.TotalDownloaded = done
		return p
	}
	got = apply(dl(10))
	got = apply(dl(50))
	got = apply(dl(99))
	if len(got.ProgressLog) != 3 {
		t.Fatalf("same-bucket download reports appended: %d", len(got.ProgressLog))
	}
	got = apply(dl(100))
	got = apply(dl(1000))
	if len(got.ProgressLog) != 5 || got.ProgressLog[4].Downloaded != 1000 {
		t.Fatalf("bucket crossings not logged: %+v", got.ProgressLog)
	}

	// cap
	for i := 0; i < ProgressLogCap+20; i++ {
		got = apply(StepProgress{Status: "INPROGRESS", Progress: i})
	}
	if len(got.ProgressLog) != ProgressLogCap {
		t.Fatalf("cap not enforced by server: %d", len(got.ProgressLog))
	}
	if got.ProgressLog[ProgressLogCap-1].Progress != ProgressLogCap+19 {
		t.Fatalf("newest line lost: %+v", got.ProgressLog[ProgressLogCap-1])
	}
	if got.StepProgress.Logs != "" {
		t.Fatalf("unexpected logs")
	}
}
