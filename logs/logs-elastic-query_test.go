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

package logs

import (
	"encoding/json"
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
func TestBuildSearchSourceAlwaysAppendsTieBreakers(t *testing.T) {
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

		for _, tieBreaker := range []string{"tsec", "tnano"} {
			if _, ok := seen[tieBreaker]; !ok {
				t.Errorf("sort %v produced no %s tiebreaker (got %v)", sort, tieBreaker, keys)
			}
		}

		// The tiebreakers have to run the same way as the primary key, or they
		// would walk the page backwards within a millisecond.
		if len(keys) > 0 {
			primary := keys[0][1]
			if seen["tsec"] != primary || seen["tnano"] != primary {
				t.Errorf("sort %v: tiebreakers %q/%q do not follow primary direction %q",
					sort, seen["tsec"], seen["tnano"], primary)
			}
		}
	}
}

// An explicit sort must keep its direction and must not be listed twice when
// it already names one of the tiebreakers.
func TestBuildSearchSourceHonoursExplicitSort(t *testing.T) {
	source := sourceOf(t, 0, 50, nil, nil, &Entry{}, Sorts{"-tsec", "-tnano"}, nil)
	keys := sortKeys(t, source)

	if len(keys) != 2 {
		t.Fatalf("expected exactly the two requested sort keys, got %v", keys)
	}
	for _, k := range keys {
		if k[1] != "desc" {
			t.Errorf("expected %s to sort desc, got %q", k[0], k[1])
		}
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
