//
// Copyright (c) 2017-2026 Pantacor Ltd.
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

package trailmodels

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// The progress log is a hard-capped history of progress changes kept on the
// step document itself, next to (never inside) "progress": the device and the
// cancel handlers replace "progress" wholesale on every report, so the log has
// to live beside it.
//
// Size budget: ProgressLogCap entries of at most ~ProgressLogMsgMax bytes plus
// a handful of numbers each, so the whole log stays in the tens of KB no
// matter how long a step lives. The cap is enforced by Mongo ($slice) on every
// append, so the array can never grow past it even under concurrent writers.
const (
	// ProgressLogCap is the maximum number of entries kept per step. Older
	// entries are dropped first.
	ProgressLogCap = 200
	// ProgressLogMsgMax bounds the status message copied into a log entry.
	ProgressLogMsgMax = 256
	// ProgressLogsFieldMax bounds the free-text "logs" a device may attach to
	// its progress report. It is the only unbounded input on the step
	// document, so it is truncated on write.
	ProgressLogsFieldMax = 64 * 1024
	// ProgressLogDownloadBuckets is how many download log lines a step gets
	// at most: one per bucket of the total download (10 => every 10%).
	ProgressLogDownloadBuckets = 10

	// ProgressLogField is the BSON/JSON key of the log on the step document.
	ProgressLogField = "progress-log"
)

// Sources of a progress log entry.
const (
	ProgressLogSourceDevice = "device"
	ProgressLogSourceOwner  = "owner"
	ProgressLogSourceHub    = "hub"
)

// ProgressLogEntry is one line of the step progress history.
type ProgressLogEntry struct {
	Time       time.Time `json:"time" bson:"time"`
	Source     string    `json:"source" bson:"source"`
	Status     string    `json:"status" bson:"status"`
	Progress   int       `json:"progress" bson:"progress"`
	StatusMsg  string    `json:"status-msg" bson:"statusmsg"`
	Retries    int       `json:"retries,omitempty" bson:"retries,omitempty"`
	Downloaded int64     `json:"downloaded,omitempty" bson:"downloaded,omitempty"`
	TotalSize  int64     `json:"total-size,omitempty" bson:"total_size,omitempty"`
}

// SanitizeStepProgress bounds the device supplied fields that would otherwise
// let a single report grow the step document without limit.
func SanitizeStepProgress(p *StepProgress) {
	if len(p.Logs) > ProgressLogsFieldMax {
		p.Logs = p.Logs[:ProgressLogsFieldMax]
	}
}

// TruncateProgressMsg bounds a status message for the log.
func TruncateProgressMsg(msg string) string {
	if len(msg) > ProgressLogMsgMax {
		return msg[:ProgressLogMsgMax]
	}
	return msg
}

// DownloadBucket maps a download position to the bucket used to decide
// whether a DOWNLOADING report deserves a new log line. Pantavisor reports
// every few seconds while bytes move; logging every report would flush the
// interesting transitions out of the capped log.
func DownloadBucket(downloaded, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return downloaded * ProgressLogDownloadBuckets / total
}

// NewProgressLogEntry builds the log line for a progress report.
func NewProgressLogEntry(p StepProgress, source string, t time.Time) ProgressLogEntry {
	e := ProgressLogEntry{
		Time:      t,
		Source:    source,
		Status:    p.Status,
		Progress:  p.Progress,
		StatusMsg: TruncateProgressMsg(p.StatusMsg),
		Retries:   p.Retries,
	}
	if p.Downloads.Total.TotalSize > 0 || p.Downloads.Total.TotalDownloaded > 0 {
		e.Downloaded = p.Downloads.Total.TotalDownloaded
		e.TotalSize = p.Downloads.Total.TotalSize
	}
	return e
}

// ProgressLogSameEvent reports whether two entries describe the same event,
// i.e. appending the second one would only repeat the first.
func ProgressLogSameEvent(a, b ProgressLogEntry) bool {
	return a.Status == b.Status &&
		a.Progress == b.Progress &&
		a.StatusMsg == b.StatusMsg &&
		a.Retries == b.Retries &&
		DownloadBucket(a.Downloaded, a.TotalSize) == DownloadBucket(b.Downloaded, b.TotalSize)
}

// AppendProgressLog appends e to log in memory, skipping repeats and keeping
// the cap. Used where the step document is rewritten as a whole.
func AppendProgressLog(log []ProgressLogEntry, e ProgressLogEntry) []ProgressLogEntry {
	if n := len(log); n > 0 && ProgressLogSameEvent(log[n-1], e) {
		return log
	}
	log = append(log, e)
	if len(log) > ProgressLogCap {
		log = log[len(log)-ProgressLogCap:]
	}
	return log
}

// ProgressUpdatePipeline builds the aggregation-pipeline update that stores a
// new progress report and appends a log line in one atomic write.
//
// The line is appended only when the report differs from the progress
// currently stored (status, progress number, message, retries or download
// bucket), so periodic identical reports do not churn the log. All values are
// wrapped in $literal because a status message or device data starting with
// "$" must never be evaluated as an expression. The array is trimmed with
// $slice to ProgressLogCap on every write.
//
// extra holds additional plain fields to $set alongside (progress-time,
// timemodified, ispublic, ...).
func ProgressUpdatePipeline(p StepProgress, source string, t time.Time, extra bson.M) mongo.Pipeline {
	entry := NewProgressLogEntry(p, source, t)

	stored := "$progress"
	storedBucket := bson.M{"$cond": bson.A{
		bson.M{"$gt": bson.A{bson.M{"$ifNull": bson.A{stored + ".downloads.total.total_size", 0}}, 0}},
		bson.M{"$floor": bson.M{"$divide": bson.A{
			bson.M{"$multiply": bson.A{
				bson.M{"$ifNull": bson.A{stored + ".downloads.total.total_downloaded", 0}},
				ProgressLogDownloadBuckets,
			}},
			stored + ".downloads.total.total_size",
		}}},
		0,
	}}

	changed := bson.M{"$or": bson.A{
		bson.M{"$ne": bson.A{stored + ".status", bson.M{"$literal": entry.Status}}},
		bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{stored + ".progress", 0}}, entry.Progress}},
		bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{stored + ".statusmsg", ""}}, bson.M{"$literal": entry.StatusMsg}}},
		bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{stored + ".retries", 0}}, entry.Retries}},
		bson.M{"$ne": bson.A{storedBucket, DownloadBucket(entry.Downloaded, entry.TotalSize)}},
	}}

	set := bson.M{
		"progress": bson.M{"$literal": p},
		ProgressLogField: bson.M{"$slice": bson.A{
			bson.M{"$concatArrays": bson.A{
				bson.M{"$ifNull": bson.A{"$" + ProgressLogField, bson.A{}}},
				bson.M{"$cond": bson.A{changed, bson.A{bson.M{"$literal": entry}}, bson.A{}}},
			}},
			-ProgressLogCap,
		}},
	}
	for k, v := range extra {
		set[k] = bson.M{"$literal": v}
	}

	return mongo.Pipeline{bson.D{{Key: "$set", Value: set}}}
}
