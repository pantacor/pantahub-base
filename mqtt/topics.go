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

// Package mqtt implements the Pantahub MQTT message plane: an embedded broker
// that lets devices receive revision notifications and user metadata as they
// happen, and report progress, device metadata and logs over a single warm
// connection instead of polling the REST API.
//
// The broker is a signalling plane only. Object blobs continue to travel over
// HTTPS signed URLs, which already handle range resume, quota and dedup.
package mqtt

import (
	"strconv"
	"strings"
)

// Topic namespace. Every topic a device may touch lives under
// Prefix + <device-id> + "/", which is what lets the ACL hook authorize by
// string prefix alone. The "v1" segment versions the payload schemas below.
const (
	Prefix        = "ph/v1/dev/"
	VersionPrefix = "ph/v1/"
)

// Topic suffixes, relative to Prefix + <device-id> + "/".
const (
	// SuffixStepsNew carries the newest actionable step for the device.
	// Retained, so a device that reconnects learns about pending work
	// immediately without asking.
	SuffixStepsNew = "steps/new"

	// SuffixUserMeta carries the full user-meta map. Retained, same reason.
	SuffixUserMeta = "user-meta"

	// SuffixDeviceMeta carries a partial device-meta map with PATCH
	// semantics: a null value unsets the key.
	SuffixDeviceMeta = "device-meta"

	// SuffixLogs carries a JSON array of log entries.
	SuffixLogs = "logs"

	// SuffixCommands carries out-of-band instructions to the device.
	SuffixCommands = "commands"

	// SuffixStatus carries device liveness. Retained, and also published by
	// the broker as the device's last will.
	SuffixStatus = "status"

	// SuffixUserMetaGet is a device REQUEST: publishing to it (empty payload)
	// asks the Hub to publish the device's full user-meta on SuffixUserMeta.
	// It lets a device seed itself on connect over the same MQTT socket instead
	// of a REST round-trip, and covers the window after a broker restart when
	// the retained user-meta has not been rebuilt yet.
	SuffixUserMetaGet = "user-meta/get"

	// SuffixStepsGet is a device REQUEST with a {"since": <rev>} payload: the
	// Hub replies on SuffixStepsNew with every trail step newer than <rev>, in
	// ascending order, so the device catches up on the whole queue of revisions
	// it missed while offline rather than only the newest one.
	SuffixStepsGet = "steps/get"
)

// progress topics are "steps/<rev>/progress".
const (
	progressPrefix = "steps/"
	progressSuffix = "/progress"
)

// Topic builds the full topic for a device and a fixed suffix.
func Topic(deviceID, suffix string) string {
	return Prefix + deviceID + "/" + suffix
}

// ProgressTopic builds the progress topic for a given trail revision.
//
// rev is always the numeric Hub trail revision. Devices whose local revision
// identifiers differ (Pantavisor requires a "locals/" prefix for locally
// installed revisions) are expected to translate before publishing, so that
// the Hub only ever sees trail revisions.
func ProgressTopic(deviceID string, rev int) string {
	return Topic(deviceID, progressPrefix+strconv.Itoa(rev)+progressSuffix)
}

// DeviceScope returns the topic prefix a device is confined to, including the
// trailing separator, so that callers can test membership with strings.HasPrefix
// without matching a device whose id merely starts with the same characters.
func DeviceScope(deviceID string) string {
	return Prefix + deviceID + "/"
}

// Parse splits a topic into the device id and the remaining suffix. ok is
// false for anything outside the versioned device namespace.
func Parse(topic string) (deviceID, suffix string, ok bool) {
	rest, found := strings.CutPrefix(topic, Prefix)
	if !found {
		return "", "", false
	}

	deviceID, suffix, found = strings.Cut(rest, "/")
	if !found || deviceID == "" || suffix == "" {
		return "", "", false
	}

	return deviceID, suffix, true
}

// ParseProgress extracts the trail revision from a "steps/<rev>/progress"
// suffix. Revisions are numeric on the Hub; anything else is rejected so a
// device cannot create step documents under an arbitrary key.
func ParseProgress(suffix string) (rev int, ok bool) {
	body, found := strings.CutPrefix(suffix, progressPrefix)
	if !found {
		return 0, false
	}

	body, found = strings.CutSuffix(body, progressSuffix)
	if !found || body == "" {
		return 0, false
	}

	rev, err := strconv.Atoi(body)
	if err != nil || rev < 0 {
		return 0, false
	}

	return rev, true
}

// DeviceMayPublish reports whether a device is allowed to publish on a suffix
// within its own namespace. Everything a device reports about itself is
// writable; everything the Hub tells the device is not.
func DeviceMayPublish(suffix string) bool {
	switch suffix {
	case SuffixDeviceMeta, SuffixLogs, SuffixStatus, SuffixUserMetaGet, SuffixStepsGet:
		return true
	}

	_, ok := ParseProgress(suffix)
	return ok
}

// DeviceMaySubscribe reports whether a device is allowed to subscribe to a
// suffix within its own namespace.
func DeviceMaySubscribe(suffix string) bool {
	switch suffix {
	case SuffixStepsNew, SuffixUserMeta, SuffixCommands:
		return true
	}
	return false
}
