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

package mqtt

import "testing"

const testDeviceID = "5c4dcf7d80123b2f2c7e96e2"

func TestParseRoundTrip(t *testing.T) {
	for _, suffix := range []string{
		SuffixStepsNew,
		SuffixUserMeta,
		SuffixDeviceMeta,
		SuffixLogs,
		SuffixCommands,
		SuffixStatus,
		"steps/7/progress",
	} {
		deviceID, got, ok := Parse(Topic(testDeviceID, suffix))
		if !ok {
			t.Errorf("Parse(%q) not recognised", suffix)
			continue
		}
		if deviceID != testDeviceID {
			t.Errorf("Parse(%q) device = %q, want %q", suffix, deviceID, testDeviceID)
		}
		if got != suffix {
			t.Errorf("Parse(%q) suffix = %q", suffix, got)
		}
	}
}

func TestParseRejectsOutsideNamespace(t *testing.T) {
	for _, topic := range []string{
		"",
		"ph/v1/dev/",
		"ph/v1/dev/" + testDeviceID,
		"ph/v1/dev/" + testDeviceID + "/",
		"ph/v2/dev/" + testDeviceID + "/logs",
		"$SYS/broker/uptime",
		"logs",
	} {
		if _, _, ok := Parse(topic); ok {
			t.Errorf("Parse(%q) accepted a topic outside the device namespace", topic)
		}
	}
}

// A device id that is a prefix of another must not gain access to it, which is
// why the ACL compares against DeviceScope rather than a bare concatenation.
func TestDeviceScopeIsNotAPrefixMatch(t *testing.T) {
	scope := DeviceScope("abc")
	if got := Topic("abcdef", SuffixLogs); len(got) > len(scope) && got[:len(scope)] == scope {
		t.Errorf("device abcdef falls inside the scope of device abc (%q)", scope)
	}
}

func TestProgressTopicParsesBack(t *testing.T) {
	_, suffix, ok := Parse(ProgressTopic(testDeviceID, 7))
	if !ok {
		t.Fatal("ProgressTopic produced an unparsable topic")
	}

	rev, ok := ParseProgress(suffix)
	if !ok {
		t.Fatalf("ParseProgress(%q) failed", suffix)
	}
	if rev != 7 {
		t.Errorf("rev = %d, want 7", rev)
	}
}

// Revisions are integers on the Hub; a step _id is "<trailID>-<rev>". Accepting
// anything else would let a device create step documents under an arbitrary key.
func TestParseProgressRejectsNonNumericRevisions(t *testing.T) {
	for _, suffix := range []string{
		"steps//progress",
		"steps/locals/hub-7/progress",
		"steps/-1/progress",
		"steps/7/progress/extra",
		"steps/7",
		"steps/new",
		"steps/0x10/progress",
		"logs",
	} {
		if rev, ok := ParseProgress(suffix); ok {
			t.Errorf("ParseProgress(%q) accepted, rev = %d", suffix, rev)
		}
	}

	if rev, ok := ParseProgress("steps/0/progress"); !ok || rev != 0 {
		t.Errorf("rev 0 must be valid, got rev = %d ok = %v", rev, ok)
	}
}

func TestDevicePermissionsAreDisjoint(t *testing.T) {
	publishable := []string{SuffixDeviceMeta, SuffixLogs, SuffixStatus, "steps/3/progress"}
	subscribable := []string{SuffixStepsNew, SuffixUserMeta, SuffixCommands}

	for _, suffix := range publishable {
		if !DeviceMayPublish(suffix) {
			t.Errorf("DeviceMayPublish(%q) = false", suffix)
		}
		if DeviceMaySubscribe(suffix) {
			t.Errorf("DeviceMaySubscribe(%q) = true, devices must not read what they report", suffix)
		}
	}

	for _, suffix := range subscribable {
		if !DeviceMaySubscribe(suffix) {
			t.Errorf("DeviceMaySubscribe(%q) = false", suffix)
		}
		if DeviceMayPublish(suffix) {
			t.Errorf("DeviceMayPublish(%q) = true, devices must not forge Hub messages", suffix)
		}
	}
}

func TestUnknownSuffixesAreDeniedBothWays(t *testing.T) {
	for _, suffix := range []string{"", "steps", "steps/new/extra", "../logs", "user-meta/key"} {
		if DeviceMayPublish(suffix) {
			t.Errorf("DeviceMayPublish(%q) = true", suffix)
		}
		if DeviceMaySubscribe(suffix) {
			t.Errorf("DeviceMaySubscribe(%q) = true", suffix)
		}
	}
}
