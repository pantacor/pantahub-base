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

package logs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sourceOf renders the query body the way it is sent to Elasticsearch, so the
// assertions below read against the actual wire shape.
func sourceOf(t *testing.T, start int64, page int64, before *time.Time, after *time.Time,
	query Filters, sort Sorts, searchAfter []interface{}) map[string]interface{} {
	t.Helper()

	src, err := buildSearchSource(start, page, before, after, query, sort, searchAfter)
	if err != nil {
		t.Fatalf("buildSearchSource failed: %s", err)
	}

	encoded, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshalling search source failed: %s", err)
	}

	out := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshalling search source failed: %s", err)
	}

	return out
}

// sortKeys flattens the "sort" clause into the field names it orders by, in
// order, each paired with its direction.
func sortKeys(t *testing.T, source map[string]interface{}) []([2]string) {
	t.Helper()

	raw, ok := source["sort"].([]interface{})
	if !ok {
		t.Fatalf("search source carries no sort clause: %v", source)
	}

	keys := []([2]string){}
	for _, entry := range raw {
		field, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected sort entry %v", entry)
		}
		for name, spec := range field {
			order := ""
			if specMap, ok := spec.(map[string]interface{}); ok {
				order, _ = specMap["order"].(string)
			}
			keys = append(keys, [2]string{name, order})
		}
	}

	return keys
}

// A caller that passes no sort must still get a defined order. This used to
// fall through to an unsorted search: every clause is a filter, so scores are
// constant and results came back in index order.
func TestBuildSearchSourceDefaultsToNewestFirst(t *testing.T) {
	source := sourceOf(t, 0, 50, nil, nil, &Entry{Owner: "prn:::accounts:/x"}, Sorts{}, nil)

	keys := sortKeys(t, source)
	if len(keys) == 0 {
		t.Fatalf("expected a default sort, got none")
	}
	if keys[0][0] != "time-created" || keys[0][1] != "desc" {
		t.Errorf("expected default sort of time-created desc, got %v", keys[0])
	}
}

// time-created is a millisecond-precision date, so a device batch puts many
// entries on one value. Without a tiebreaker their relative order is decided
// per shard and is not reproducible, which makes search_after unable to tell
// where a page ended.
func TestBuildSearchSourceAlwaysAppendsTieBreaker(t *testing.T) {
	for _, sort := range []Sorts{
		{},
		{"time-created"},
		{"-time-created"},
		{"-lvl"},
	} {
		source := sourceOf(t, 0, 50, nil, nil, &Entry{}, sort, nil)
		keys := sortKeys(t, source)

		seen := map[string]string{}
		for _, k := range keys {
			seen[k[0]] = k[1]
		}

		direction, ok := seen[sortTieBreaker]
		if !ok {
			t.Errorf("sort %v produced no %s tiebreaker (got %v)", sort, sortTieBreaker, keys)
			continue
		}

		// The tiebreaker has to run the same way as the primary key, or it
		// would walk the page backwards within a millisecond.
		if len(keys) > 0 && direction != keys[0][1] {
			t.Errorf("sort %v: tiebreaker %q does not follow primary direction %q",
				sort, direction, keys[0][1])
		}
	}
}

// The tiebreaker must only name fields that exist on every index the search
// spans. tsec/tnano are tagged omitempty on Entry and are absent from most
// stored documents; sorting on one raises "No mapping found for [tsec] in
// order to sort on", and because the search runs against an index wildcard
// that comes back as a 200 with partially failed shards -- entries silently
// missing from every index lacking the field. The ObjectID is set on ingest
// unconditionally, so it is present everywhere and is unique per entry.
func TestBuildSearchSourceTieBreakerIsAlwaysPresentAndUnique(t *testing.T) {
	source := sourceOf(t, 0, 50, nil, nil, &Entry{}, Sorts{}, nil)

	for _, k := range sortKeys(t, source) {
		if k[0] == "tsec" || k[0] == "tnano" {
			t.Errorf("%s is absent from most documents and must not be sorted on", k[0])
		}
	}

	if sortTieBreaker != "id.keyword" {
		t.Errorf("expected the ObjectID keyword subfield as tiebreaker, got %q", sortTieBreaker)
	}

	// A missing mapping must degrade instead of failing the shard.
	raw, _ := json.Marshal(source["sort"])
	if !strings.Contains(string(raw), "unmapped_type") {
		t.Errorf("tiebreaker carries no unmapped_type guard: %s", raw)
	}
}

// An explicit sort must keep its direction and must not be listed twice when
// it already names the tiebreaker.
func TestBuildSearchSourceHonoursExplicitSort(t *testing.T) {
	source := sourceOf(t, 0, 50, nil, nil, &Entry{}, Sorts{"-lvl"}, nil)
	keys := sortKeys(t, source)

	if len(keys) != 2 {
		t.Fatalf("expected the requested key plus one tiebreaker, got %v", keys)
	}
	if keys[0][0] != "lvl" || keys[0][1] != "desc" {
		t.Errorf("explicit sort key was not preserved: %v", keys[0])
	}
	if keys[1][0] != sortTieBreaker {
		t.Errorf("expected %s as second key, got %v", sortTieBreaker, keys[1])
	}
}

// Naming the tiebreaker explicitly must not list it twice.
func TestBuildSearchSourceDoesNotDuplicateTieBreaker(t *testing.T) {
	source := sourceOf(t, 0, 50, nil, nil, &Entry{}, Sorts{"-time-created", "-" + sortTieBreaker}, nil)
	keys := sortKeys(t, source)

	if len(keys) != 2 {
		t.Fatalf("expected exactly two sort keys, got %v", keys)
	}
}

// search_after is the keyset bound; Elasticsearch rejects it together with a
// from offset, so the offset has to be dropped when a cursor is in play.
func TestBuildSearchSourceSearchAfterReplacesOffset(t *testing.T) {
	after := []interface{}{float64(1700000000000), float64(1700000000), float64(12345)}
	source := sourceOf(t, 120, 50, nil, nil, &Entry{}, Sorts{"-time-created"}, after)

	if _, ok := source["from"]; ok {
		t.Errorf("search_after must not be combined with a from offset: %v", source)
	}

	got, ok := source["search_after"].([]interface{})
	if !ok {
		t.Fatalf("expected a search_after clause, got %v", source)
	}
	if len(got) != len(after) {
		t.Fatalf("expected %d search_after values, got %v", len(after), got)
	}
}

// Without a cursor, offset paging still has to work for plain API consumers.
func TestBuildSearchSourceKeepsOffsetWithoutCursor(t *testing.T) {
	source := sourceOf(t, 120, 50, nil, nil, &Entry{}, Sorts{"-time-created"}, nil)

	from, ok := source["from"].(float64)
	if !ok || int(from) != 120 {
		t.Errorf("expected from=120, got %v", source["from"])
	}
}

// Both time bounds have to survive together; the mongo backend used to let the
// second one overwrite the first.
func TestBuildSearchSourceKeepsBothTimeBounds(t *testing.T) {
	before := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	after := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)

	source := sourceOf(t, 0, 50, &before, &after, &Entry{}, Sorts{}, nil)

	// Rendered as two separate range clauses over time-created, one bounding
	// each end (the client serialises a range as from/to plus inclusivity).
	query, ok := source["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("no query in source: %v", source)
	}
	boolQuery, ok := query["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("no bool query: %v", query)
	}
	filters, ok := boolQuery["filter"].([]interface{})
	if !ok {
		t.Fatalf("no filter clauses: %v", boolQuery)
	}

	upperBound, lowerBound := false, false
	for _, clause := range filters {
		asMap, ok := clause.(map[string]interface{})
		if !ok {
			continue
		}
		rangeClause, ok := asMap["range"].(map[string]interface{})
		if !ok {
			continue
		}
		spec, ok := rangeClause["time-created"].(map[string]interface{})
		if !ok {
			continue
		}
		if to, ok := spec["to"].(string); ok && to != "" {
			upperBound = true
		}
		if from, ok := spec["from"].(string); ok && from != "" {
			lowerBound = true
		}
	}

	if !upperBound {
		t.Errorf("the 'before' bound was dropped: %v", filters)
	}
	if !lowerBound {
		t.Errorf("the 'after' bound was dropped: %v", filters)
	}
}
