// Copyright (c) 2026 Pantacor Ltd.
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

// Package stateops edits Pantavisor state documents the way pvr and the web
// app's pvtx editor do: parts are removed with their signature, copied from
// another state with the signatures that cover them, and pvr exports are
// merged in. The merging itself is libpvr's, so a state changed here is the
// state pvr would have produced.
//
// Nothing here verifies a signature. The device does that; Signatures only
// tells which files a signature protects, so a caller can warn before posting
// a state whose signed files changed.
package stateops

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gitlab.com/pantacor/pvr/libpvr"
	"gitlab.com/pantacor/pvr/utils/pvjson"
)

const (
	specKey     = "#spec"
	sigsPrefix  = "_sigs/"
	configDir   = "_config/"
	inlineJSON  = ".json"
	sigFileType = ".json"
)

// ErrInvalid marks a refusal that is about the requested change. Its message
// is written for whoever asked.
var ErrInvalid = errors.New("invalid state change")

func invalid(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Part groups the keys of a state the way pvr names them: an app or the BSP
// by its directory, its configuration overlay as _config/<name>, each
// signature by its _sigs/<name>.json key, and root documents by their key.
type Part struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind" jsonschema:"app, config, signature or document"`
	Files []string `json:"files"`
	// SignedBy lists the signatures that protect any file of the part.
	SignedBy []string `json:"signed_by,omitempty"`
}

// Signature is what one signature of a state covers.
type Signature struct {
	Key      string   `json:"key"`
	Protects []string `json:"protects"`
	// Excludes are files its patterns name but deliberately leave out, such
	// as a configuration overlay signed apart with --noconfig.
	Excludes []string `json:"excludes,omitempty"`
	// Unreadable is set when the signature could not be parsed; the device
	// will not accept it either.
	Unreadable string `json:"unreadable,omitempty"`
}

// PartOf names the part a state key belongs to.
func PartOf(key string) string {
	switch {
	case key == specKey, strings.HasPrefix(key, sigsPrefix):
		return key
	case strings.HasPrefix(key, configDir):
		rest := strings.TrimPrefix(key, configDir)
		if i := strings.Index(rest, "/"); i >= 0 {
			return configDir + rest[:i]
		}
		return key
	}
	if i := strings.Index(key, "/"); i >= 0 {
		return key[:i]
	}
	return key
}

func kindOf(part string) string {
	switch {
	case strings.HasPrefix(part, sigsPrefix):
		return "signature"
	case strings.HasPrefix(part, configDir):
		return "config"
	case !strings.Contains(part, "/") && !strings.HasSuffix(part, inlineJSON) && part != specKey:
		return "app"
	}
	return "document"
}

// Signatures reports every signature of state and the files it covers.
func Signatures(state map[string]interface{}) []Signature {
	sigs := []Signature{}
	for _, key := range sortedKeys(state) {
		if !strings.HasPrefix(key, sigsPrefix) {
			continue
		}
		protected, excluded, err := libpvr.SignatureCoverage(state, key)
		sig := Signature{Key: key, Protects: protected, Excludes: excluded}
		if err != nil {
			sig.Unreadable = err.Error()
			sig.Protects = []string{}
		}
		sigs = append(sigs, sig)
	}
	return sigs
}

// Parts describes state part by part, with the signatures protecting each.
func Parts(state map[string]interface{}) []Part {
	signedBy := map[string]map[string]bool{}
	for _, sig := range Signatures(state) {
		for _, file := range sig.Protects {
			part := PartOf(file)
			if signedBy[part] == nil {
				signedBy[part] = map[string]bool{}
			}
			signedBy[part][sig.Key] = true
		}
	}

	byName := map[string]*Part{}
	for _, key := range sortedKeys(state) {
		name := PartOf(key)
		part, ok := byName[name]
		if !ok {
			part = &Part{Name: name, Kind: kindOf(name), Files: []string{}}
			byName[name] = part
		}
		part.Files = append(part.Files, key)
	}

	parts := make([]Part, 0, len(byName))
	for _, name := range sortedKeys(byName) {
		part := byName[name]
		for sig := range signedBy[name] {
			part.SignedBy = append(part.SignedBy, sig)
		}
		sort.Strings(part.SignedBy)
		parts = append(parts, *part)
	}
	return parts
}

// sigKeyOf is where pvr keeps the signature of an app.
func sigKeyOf(name string) string {
	return sigsPrefix + name + sigFileType
}

func hasPrefix(state map[string]interface{}, prefix string) bool {
	for key := range state {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func deletePrefix(state map[string]interface{}, prefix string) {
	for key := range state {
		if strings.HasPrefix(key, prefix) {
			delete(state, key)
		}
	}
}

// normalizePart turns what a caller named into a part name: "myapp/" and
// "myapp" are the same app.
func normalizePart(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), "/")
}

// RemoveParts returns state without the named parts. An app goes with its
// configuration overlay and its signature, and removing a signature takes the
// files it protects with it, as pvr does.
func RemoveParts(state map[string]interface{}, names []string) (map[string]interface{}, error) {
	if len(names) == 0 {
		return nil, invalid("name at least one part to remove")
	}
	result, err := clone(state)
	if err != nil {
		return nil, err
	}

	for _, raw := range names {
		name := normalizePart(raw)
		switch {
		case name == "" || name == specKey:
			return nil, invalid("%q cannot be removed", raw)

		case strings.HasPrefix(name, sigsPrefix):
			if _, ok := result[name]; !ok {
				return nil, invalid("there is no signature %q", name)
			}
			if err := deleteSignature(result, name); err != nil {
				return nil, err
			}

		default:
			found := false
			if _, ok := result[name]; ok && !strings.Contains(name, "/") {
				delete(result, name)
				found = true
			}
			if _, ok := result[sigKeyOf(name)]; ok {
				if err := deleteSignature(result, sigKeyOf(name)); err != nil {
					return nil, err
				}
				found = true
			}
			for _, prefix := range []string{name + "/", configDir + name + "/"} {
				if hasPrefix(result, prefix) {
					deletePrefix(result, prefix)
					found = true
				}
			}
			if !found {
				return nil, invalid("there is no part %q", name)
			}
		}
	}
	return result, nil
}

// deleteSignature removes a signature and the files only it protects, with
// libpvr's DeleteSignatureFromState as pvr's app removal does.
func deleteSignature(state map[string]interface{}, sigKey string) error {
	signature, ok := state[sigKey].(map[string]interface{})
	if !ok {
		delete(state, sigKey)
		return nil
	}
	buf, err := pvjson.Marshal(state, pvjson.MarshalOptions{Compact: true})
	if err != nil {
		return err
	}
	if _, err := libpvr.DeleteSignatureFromState(state, buf, sigKey, signature, nil); err != nil {
		return invalid("the signature %s cannot be read: %s", sigKey, err.Error())
	}
	delete(state, sigKey)
	return nil
}

// CopyParts returns dst with the named parts taken from src, replacing what
// dst had under the same names. Each app comes with its configuration overlay
// and its signature, and a signature brings every file it protects, which is
// what pvr does for "pvr get <state>#<parts>".
func CopyParts(dst, src map[string]interface{}, names []string) (map[string]interface{}, error) {
	if len(names) == 0 {
		return nil, invalid("name at least one part to copy")
	}

	base, err := clone(dst)
	if err != nil {
		return nil, err
	}

	frags := []string{}
	for _, raw := range names {
		name := normalizePart(raw)
		switch {
		case name == "" || name == specKey:
			return nil, invalid("%q cannot be copied", raw)
		case strings.HasPrefix(name, sigsPrefix):
			if _, ok := src[name]; !ok {
				return nil, invalid("the source has no signature %q", name)
			}
			frags = append(frags, name)
		default:
			_, isDocument := src[name]
			hasDir := hasPrefix(src, name+"/")
			if !isDocument && !hasDir {
				return nil, invalid("the source has no part %q", name)
			}
			frags = append(frags, name)
			// What dst had of the part goes, so that files the source
			// dropped do not linger.
			deletePrefix(base, name+"/")
			if hasPrefix(src, configDir+name+"/") {
				frags = append(frags, configDir+name)
				deletePrefix(base, configDir+name+"/")
			}
			if _, ok := src[sigKeyOf(name)]; ok {
				frags = append(frags, sigKeyOf(name))
			}
		}
	}

	expanded, err := libpvr.ExpandFragmentsWithSignedParts(src, strings.Join(frags, ","))
	if err != nil {
		return nil, invalid("a signature of the source cannot be read: %s", err.Error())
	}
	return patch(base, src, expanded)
}

// Import returns dst with a pvr export merged in, as the web app's pvtx
// editor merges one: every part the export brings replaces the part of that
// name, and a signature it brings replaces the one it supersedes together
// with the files that one protected.
func Import(dst, export map[string]interface{}) (map[string]interface{}, error) {
	if len(export) == 0 {
		return nil, invalid("the export holds no state")
	}
	base, err := clone(dst)
	if err != nil {
		return nil, err
	}
	for key := range export {
		name := PartOf(key)
		if name == key {
			continue // a root document or a signature replaces itself
		}
		deletePrefix(base, name+"/")
	}
	return patch(base, export, "")
}

// patch applies libpvr.PatchState as pvr does: srcFrags empty, the parts to
// take named by frags (empty takes everything).
func patch(dst, src map[string]interface{}, frags string) (map[string]interface{}, error) {
	dstBuf, err := pvjson.Marshal(dst, pvjson.MarshalOptions{Canonical: true})
	if err != nil {
		return nil, err
	}
	srcBuf, err := pvjson.Marshal(src, pvjson.MarshalOptions{Canonical: true})
	if err != nil {
		return nil, err
	}
	_, merged, err := libpvr.PatchState(dstBuf, srcBuf, "", frags, false, nil)
	if err != nil {
		return nil, invalid("the states cannot be merged: %s", err.Error())
	}
	return clone(merged)
}

// SetDocument returns state with the inline JSON document at path set to
// value. Only documents (keys ending in .json) are held inline; binaries are
// objects and change by copying or importing the part they belong to.
func SetDocument(state map[string]interface{}, path string, value interface{}) (map[string]interface{}, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "" || path == specKey:
		return nil, invalid("%q cannot be set", path)
	case strings.HasPrefix(path, sigsPrefix):
		return nil, invalid("signatures are made with pvr and arrive by copying or importing the part they sign")
	case !strings.HasSuffix(path, inlineJSON):
		return nil, invalid("only JSON documents (paths ending in .json) are held in the state; %q is a binary object", path)
	case value == nil:
		return nil, invalid("give the document a value; use delete to remove it")
	}
	if _, err := pvjson.Marshal(value); err != nil {
		return nil, invalid("the value is not a JSON document: %s", err.Error())
	}

	result, err := clone(state)
	if err != nil {
		return nil, err
	}
	result[path] = value
	return clone(result)
}

// DeleteFile returns state without the file at path.
func DeleteFile(state map[string]interface{}, path string) (map[string]interface{}, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "" || path == specKey:
		return nil, invalid("%q cannot be deleted", path)
	case strings.HasPrefix(path, sigsPrefix):
		return nil, invalid("remove a signature as a part, which also removes the files it protects")
	}
	if _, ok := state[path]; !ok {
		return nil, invalid("there is no file %q", path)
	}
	result, err := clone(state)
	if err != nil {
		return nil, err
	}
	delete(result, path)
	return result, nil
}

// Diff is what changed between two states, key by key.
type Diff struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}

// Empty reports whether nothing changed.
func (d Diff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

// Compare tells what changed from before to after.
func Compare(before, after map[string]interface{}) Diff {
	diff := Diff{Added: []string{}, Removed: []string{}, Changed: []string{}}
	for _, key := range sortedKeys(after) {
		old, ok := before[key]
		if !ok {
			diff.Added = append(diff.Added, key)
		} else if !sameValue(old, after[key]) {
			diff.Changed = append(diff.Changed, key)
		}
	}
	for _, key := range sortedKeys(before) {
		if _, ok := after[key]; !ok {
			diff.Removed = append(diff.Removed, key)
		}
	}
	return diff
}

// SignatureWarnings tells, before a state is posted, where the device is
// likely to refuse it: a signature that stayed the same while files it
// protects changed, a signature that cannot be read, and parts that nothing
// signs any more on a device whose state was signed.
func SignatureWarnings(before, after map[string]interface{}) []string {
	warnings := []string{}

	beforeSigs := map[string]Signature{}
	for _, sig := range Signatures(before) {
		beforeSigs[sig.Key] = sig
	}

	signedAfter := map[string]bool{}
	excludedAfter := map[string]bool{}
	for _, sig := range Signatures(after) {
		if sig.Unreadable != "" {
			warnings = append(warnings, fmt.Sprintf("%s cannot be read (%s); the device will refuse it", sig.Key, sig.Unreadable))
			continue
		}
		for _, file := range sig.Protects {
			signedAfter[file] = true
		}
		// A file a signature names but excludes (pvr excludes src.json, and
		// --noconfig excludes the overlay) is left out on purpose, not
		// unsigned.
		for _, file := range sig.Excludes {
			excludedAfter[file] = true
		}

		old, existed := beforeSigs[sig.Key]
		if !existed || !sameValue(before[sig.Key], after[sig.Key]) {
			continue // a new or replaced signature was made for these files
		}
		changed := []string{}
		for _, file := range union(old.Protects, sig.Protects) {
			if !sameValue(before[file], after[file]) {
				changed = append(changed, file)
			}
		}
		if len(changed) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"%s is unchanged but files it protects changed (%s); the device will refuse the state unless it is signed again",
				sig.Key, strings.Join(changed, ", ")))
		}
	}

	if len(beforeSigs) > 0 {
		// A part counts as signed when a signature protects any of its files:
		// the ones it leaves out are excluded on purpose.
		signedParts := map[string]bool{}
		for file := range signedAfter {
			signedParts[PartOf(file)] = true
		}

		unsigned := map[string]bool{}
		for key := range after {
			part := PartOf(key)
			if key == specKey || strings.HasPrefix(key, sigsPrefix) || signedAfter[key] ||
				excludedAfter[key] || signedParts[part] {
				continue
			}
			if _, wasThere := before[key]; wasThere && !isProtected(beforeSigs, key) {
				continue // it was not signed before either
			}
			unsigned[part] = true
		}
		if len(unsigned) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"no signature protects %s; a device that requires signed states will refuse it",
				strings.Join(sortedKeys(unsigned), ", ")))
		}
	}
	return warnings
}

func isProtected(sigs map[string]Signature, key string) bool {
	for _, sig := range sigs {
		for _, file := range sig.Protects {
			if file == key {
				return true
			}
		}
	}
	return false
}

// Validate refuses a state no device could run.
func Validate(state map[string]interface{}) error {
	spec, ok := state[specKey].(string)
	if !ok || spec == "" {
		return invalid("the state has no #spec")
	}
	for key, value := range state {
		if key == specKey || strings.HasSuffix(key, inlineJSON) {
			continue
		}
		if _, ok := value.(string); !ok {
			return invalid("%s must reference an object by its sha256", key)
		}
	}
	return nil
}

func sameValue(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aBuf, errA := pvjson.Marshal(a, pvjson.MarshalOptions{Canonical: true})
	bBuf, errB := pvjson.Marshal(b, pvjson.MarshalOptions{Canonical: true})
	return errA == nil && errB == nil && string(aBuf) == string(bBuf)
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	for _, v := range append(append([]string{}, a...), b...) {
		seen[v] = true
	}
	return sortedKeys(seen)
}

// clone copies state decoded the way the REST API decodes a posted one
// (numbers as float64), so a state produced here is stored exactly as the
// same state posted through POST /trails/:id/steps would be. libpvr decodes
// numbers as json.Number, which would otherwise reach the database as text.
func clone(state map[string]interface{}) (map[string]interface{}, error) {
	buf, err := pvjson.Marshal(state, pvjson.MarshalOptions{Compact: true})
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{}
	if err := json.Unmarshal(buf, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
