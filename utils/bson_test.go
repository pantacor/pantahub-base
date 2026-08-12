// Copyright (c) 2026 Pantacor Ltd.
//
// Tests for BsonQuoteMap / BsonUnquoteMap. The invariants we care about:
//
//  1. Recursion: nested map keys at every depth are escaped / unescaped
//     (the pre-fix implementation only touched top-level keys).
//  2. Key encoding: both `.` and `$` in keys are replaced with sentinels
//     (U+FF2E and U+FFE0 respectively).
//  3. Value encoding asymmetry (Pantahub canonical rule):
//     - `$` in a string value IS escaped to U+FFE0 — "no raw $
//     anywhere in a stored doc" is the historic rule that matches
//     pantahub-base's encoder, so aggregations / $expr / rewrites
//     can never misinterpret a value as an operator reference.
//     - `.` in a string value is LEFT RAW — dotted-string user data
//     (URLs, pantavisor.revision, component paths) must round-trip
//     untouched.
//  4. Idempotence: quote(quote(x)) == quote(x); unquote(unquote(x)) == unquote(x).
//  5. Round-trip: unquote(quote(x)) == x. Fully reversible because the
//     `.→Ｎ` transform never happens on a value, and `$↔￠` is
//     bijective.
//  6. Arrays of maps: elements get walked; array shape preserved.
//  7. primitive.M / primitive.A inputs accepted (mongo driver
//     decodes into those by default); output is normalised to
//     map[string]interface{} / []interface{}.
//  8. Collisions: when two distinct raw keys fold to the same escaped
//     key, one wins and we don't panic or duplicate.
//  9. Values are opaque: Ｎ in a value is NEVER rewritten on read.
//     It could be legitimate user data (a label-ified rendering of a
//     quoted key, a pasted string) or — rarely — a pre-fix leak. The
//     runtime unquoter must not guess; let hubmanager bsonfix surface
//     true leaks for operator review.
package utils

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestBsonQuoteMap_TopLevelKeys(t *testing.T) {
	in := map[string]interface{}{
		"plain":            "value",
		"dotted.key":       "v1",
		"dollar$key":       "v2",
		"both.$mixed":      "v3",
		"already Ｎescaped": "v4", // already-escaped → no-op
	}
	got := BsonQuoteMap(&in)

	want := map[string]interface{}{
		"plain":            "value",
		"dottedＮkey":       "v1",
		"dollar￠key":       "v2",
		"bothＮ￠mixed":      "v3",
		"already Ｎescaped": "v4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level quote mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestBsonQuoteMap_ValueEncodingAsymmetry(t *testing.T) {
	// Pantahub canonical rule:
	//   - $ in values → ￠ (so aggregations / $expr / query rewriting
	//     never misinterpret a value as an operator).
	//   - . in values → LEFT RAW (dotted-string user data must survive
	//     untouched).
	in := map[string]interface{}{
		"url":              "https://example.com/a.b/c", // . must survive in value
		"template":         "${VAR}-${OTHER}",           // $ must be escaped
		"jsonpath":         "$.foo[*].bar",              // mixed: $ escaped, . preserved
		"with.dot.in.key":  "nothing.in.value",          // key escape, value . preserved
		"with.dollar.key$": "plain",                     // both escape in key, value untouched
	}
	got := BsonQuoteMap(&in)

	wantValues := map[string]string{
		"url":              "https://example.com/a.b/c",
		"template":         "￠{VAR}-￠{OTHER}",
		"jsonpath":         "￠.foo[*].bar",
		"withＮdotＮinＮkey":  "nothing.in.value",
		"withＮdollarＮkey￠": "plain",
	}
	if len(got) != len(wantValues) {
		t.Fatalf("key-count mismatch: got %d, want %d; got=%#v", len(got), len(wantValues), got)
	}
	for k, wantV := range wantValues {
		gotV, ok := got[k]
		if !ok {
			t.Errorf("missing key %q in output; got=%#v", k, got)
			continue
		}
		if gotV != wantV {
			t.Errorf("value for key %q: got %q, want %q", k, gotV, wantV)
		}
	}
}

func TestBsonQuoteMap_NestedMaps(t *testing.T) {
	// Recursion invariant. Pre-fix implementation only escaped
	// top-level keys via bsonQuoteMap; nested `.` keys survived into
	// mongo producing RAW_DOT_IN_QUOTED_KEY findings (nested `$`
	// got side-escaped via the marshal/global-replace pass).
	in := map[string]interface{}{
		"state": map[string]interface{}{
			"resmon": map[string]interface{}{
				"src.json": map[string]interface{}{
					"dm_enabled": map[string]interface{}{
						"root.squashfs": true,
						"$cmd":          "noop-with-$-in-value",
					},
				},
			},
		},
	}
	got := BsonQuoteMap(&in)

	outer := got["state"].(map[string]interface{})
	resmon := outer["resmon"].(map[string]interface{})
	srcJSON, ok := resmon["srcＮjson"].(map[string]interface{})
	if !ok {
		t.Fatalf("srcＮjson not a map: keys=%v", keysOf(resmon))
	}
	dmEnabled, ok := srcJSON["dm_enabled"].(map[string]interface{})
	if !ok {
		t.Fatalf("dm_enabled not a map: keys=%v", keysOf(srcJSON))
	}
	if _, ok := dmEnabled["rootＮsquashfs"]; !ok {
		t.Errorf("deep key rootＮsquashfs missing; keys=%v", keysOf(dmEnabled))
	}
	cmdVal, ok := dmEnabled["￠cmd"]
	if !ok {
		t.Fatalf("deep key ￠cmd missing; keys=%v", keysOf(dmEnabled))
	}
	if cmdVal != "noop-with-￠-in-value" {
		t.Errorf("deep value not escaped: got %q", cmdVal)
	}
}

func TestBsonQuoteMap_ArraysOfMaps(t *testing.T) {
	in := map[string]interface{}{
		"components": []interface{}{
			map[string]interface{}{
				"step": map[string]interface{}{
					"state": map[string]interface{}{
						"root.squashfs": "rootfs-path-with-a.b/c",
					},
				},
			},
			map[string]interface{}{
				"step": map[string]interface{}{
					"state": map[string]interface{}{
						"$foo": "${EVAL}",
					},
				},
			},
		},
	}
	got := BsonQuoteMap(&in)

	arr := got["components"].([]interface{})
	if len(arr) != 2 {
		t.Fatalf("length: got %d, want 2", len(arr))
	}
	state0 := arr[0].(map[string]interface{})["step"].(map[string]interface{})["state"].(map[string]interface{})
	vRaw, ok := state0["rootＮsquashfs"]
	if !ok {
		t.Fatalf("arr[0] deep key missing; got=%v", keysOf(state0))
	}
	if vRaw != "rootfs-path-with-a.b/c" {
		t.Errorf("arr[0] value `.`-preservation failed: got %q", vRaw)
	}
	state1 := arr[1].(map[string]interface{})["step"].(map[string]interface{})["state"].(map[string]interface{})
	vEsc, ok := state1["￠foo"]
	if !ok {
		t.Fatalf("arr[1] deep key missing; got=%v", keysOf(state1))
	}
	if vEsc != "￠{EVAL}" {
		t.Errorf("arr[1] value `$`-escape failed: got %q", vEsc)
	}
}

func TestBsonQuoteMap_PrimitiveMPrimitiveA(t *testing.T) {
	// mongo-go-driver decodes nested documents into primitive.M and
	// arrays into primitive.A by default.
	in := map[string]interface{}{
		"doc": primitive.M{
			"dotted.key": "no.escape.in.value",
			"arr": primitive.A{
				primitive.M{"$foo": "bar-$-raw"},
			},
		},
	}
	got := BsonQuoteMap(&in)

	doc, ok := got["doc"].(map[string]interface{})
	if !ok {
		t.Fatalf("doc not map[string]interface{}: %T", got["doc"])
	}
	if got["doc"].(map[string]interface{})["dottedＮkey"] != "no.escape.in.value" {
		t.Errorf("primitive.M value `.` not preserved: got %v", doc["dottedＮkey"])
	}
	arr, ok := doc["arr"].([]interface{})
	if !ok {
		t.Fatalf("arr not []interface{}: %T", doc["arr"])
	}
	inner := arr[0].(map[string]interface{})
	if inner["￠foo"] != "bar-￠-raw" {
		t.Errorf("primitive.A nested value `$`-escape failed: got %v", inner["￠foo"])
	}
}

func TestBsonQuoteMap_Idempotent(t *testing.T) {
	in := map[string]interface{}{
		"a.b": map[string]interface{}{
			"$c": "raw$value",
		},
	}
	once := BsonQuoteMap(&in)
	twice := BsonQuoteMap(&once)

	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("not idempotent:\n once=%#v\ntwice=%#v", once, twice)
	}
}

func TestBsonQuoteMap_NilInput(t *testing.T) {
	if got := BsonQuoteMap(nil); got != nil {
		t.Fatalf("BsonQuoteMap(nil) = %v, want nil", got)
	}
}

func TestBsonQuoteMap_DoesNotMutateInput(t *testing.T) {
	in := map[string]interface{}{
		"dotted.key": map[string]interface{}{"$nested": "$val"},
	}
	_ = BsonQuoteMap(&in)
	if _, ok := in["dotted.key"]; !ok {
		t.Errorf("input was mutated: top-level raw key lost; keys=%v", keysOf(in))
	}
	inner := in["dotted.key"].(map[string]interface{})
	if v, ok := inner["$nested"]; !ok || v != "$val" {
		t.Errorf("input nested map was mutated: got %v, want $val", v)
	}
}

func TestBsonUnquoteMap_TopLevelKeys(t *testing.T) {
	in := map[string]interface{}{
		"dottedＮkey":  "v1",
		"dollar￠key":  "v2",
		"bothＮ￠mixed": "v3",
		"plain":       "v4",
	}
	got := BsonUnquoteMap(&in)

	want := map[string]interface{}{
		"dotted.key":  "v1",
		"dollar$key":  "v2",
		"both.$mixed": "v3",
		"plain":       "v4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unquote mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestBsonUnquoteMap_ValueSide(t *testing.T) {
	// `$` round-trip: ￠ in a value IS our escape, so it reverses.
	// Ｎ in a value is opaque data (could be a pre-quoted key rendered
	// into a label, a pasted string, etc.) and must round-trip
	// unchanged — the runtime unquoter doesn't get to guess.
	// `.` in a value never gets touched either direction.
	in := map[string]interface{}{
		"clean":            "￠{VAR}",                  // canonical: ￠ in value, reversed
		"opaque_n":         "pvwificonnect/srcＮjson",  // Ｎ in value: legitimate, preserved
		"mixed":            "￠cmd with Ｎdot and ￠ref", // ￠ reversed, Ｎ preserved
		"dotted.preserved": "a.b.c",                   // `.` untouched in value
	}
	got := BsonUnquoteMap(&in)

	wantValues := map[string]string{
		"clean":            "${VAR}",
		"opaque_n":         "pvwificonnect/srcＮjson",
		"mixed":            "$cmd with Ｎdot and $ref",
		"dotted.preserved": "a.b.c",
	}
	for k, wantV := range wantValues {
		if got[k] != wantV {
			t.Errorf("value for key %q: got %q, want %q", k, got[k], wantV)
		}
	}
}

func TestBsonQuoteUnquoteRoundTrip(t *testing.T) {
	// unquote(quote(x)) == x for any string-keyed / string-or-
	// composite-valued input. Critical: dotted STRING VALUES must
	// survive verbatim.
	//
	// Numeric caveat: JSON round-trip normalises int → float64, so
	// this test uses the normalised shape as its `want`. Callers
	// that need int fidelity should go through typed struct fields,
	// not through map[string]interface{}.
	in := map[string]interface{}{
		"a.b": map[string]interface{}{
			"$nested":     "keep-$-reversible",
			"also.dotted": 42, // int in input
			"url":         "https://hub.pantahub.com/v1/foo.bar",
			"arr": []interface{}{
				map[string]interface{}{"x.y": "leaf.with.dots"},
			},
		},
	}
	want := map[string]interface{}{
		"a.b": map[string]interface{}{
			"$nested":     "keep-$-reversible",
			"also.dotted": float64(42), // normalised by JSON round-trip
			"url":         "https://hub.pantahub.com/v1/foo.bar",
			"arr": []interface{}{
				map[string]interface{}{"x.y": "leaf.with.dots"},
			},
		},
	}
	q := BsonQuoteMap(&in)
	back := BsonUnquoteMap(&q)

	if !reflect.DeepEqual(back, want) {
		t.Fatalf("round-trip mismatch:\n got=%#v\nwant=%#v", back, want)
	}
}

func TestBsonUnquoteMap_Idempotent(t *testing.T) {
	in := map[string]interface{}{
		"aＮb": map[string]interface{}{"￠c": "￠val"},
	}
	once := BsonUnquoteMap(&in)
	twice := BsonUnquoteMap(&once)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("not idempotent:\n once=%#v\ntwice=%#v", once, twice)
	}
}

func TestBsonQuoteMap_Collision(t *testing.T) {
	// Two distinct raw keys folding to the same escaped form. First-
	// encounter wins; total key count drops by one and we don't panic.
	in := map[string]interface{}{
		"a.b": 1,
		"aＮb": 2,
	}
	got := BsonQuoteMap(&in)
	if len(got) != 1 {
		t.Fatalf("collision not collapsed: got %d keys, want 1 (%v)", len(got), got)
	}
	if _, ok := got["aＮb"]; !ok {
		t.Errorf("expected survivor key aＮb; got keys=%v", keysOf(got))
	}
}

func TestBsonQuoteMap_NonStringLeaves_AfterJSONRoundTrip(t *testing.T) {
	// The hybrid approach routes everything through a json.Marshal /
	// json.Unmarshal pair for the `$→￠` step. That means non-JSON-
	// native types normalise to their JSON representations:
	//
	//   - primitive.ObjectID -> its hex string (via MarshalJSON)
	//   - int                -> float64 (JSON has no int)
	//   - bool               -> bool (unchanged)
	//   - nil                -> nil (unchanged)
	//   - primitive.A        -> []interface{} with element-wise
	//                           JSON normalisation
	//
	// This matches the legacy cjson-based BsonQuoteMap behaviour the
	// hybrid is consciously preserving. Callers that need preservation
	// of primitive.ObjectID / int64 / time.Time etc. must not route
	// those through BsonQuoteMap — they belong on typed struct fields
	// that flow through the mongo driver's BSON codec directly.
	oid := primitive.NewObjectID()
	in := map[string]interface{}{
		"dotted.key": map[string]interface{}{
			"id":   oid,
			"num":  42,
			"flag": true,
			"nil":  nil,
			"list": primitive.A{1, 2, 3},
		},
	}
	got := BsonQuoteMap(&in)
	inner := got["dottedＮkey"].(map[string]interface{})

	if inner["id"] != oid.Hex() {
		t.Errorf("ObjectID: got %v, want hex %q", inner["id"], oid.Hex())
	}
	if inner["num"] != float64(42) {
		t.Errorf("int->float64: got %T(%v), want float64(42)", inner["num"], inner["num"])
	}
	if inner["flag"] != true {
		t.Errorf("bool mutated: got %v", inner["flag"])
	}
	if inner["nil"] != nil {
		t.Errorf("nil mutated: got %v", inner["nil"])
	}
	list, ok := inner["list"].([]interface{})
	if !ok {
		t.Fatalf("list not []interface{}: %T", inner["list"])
	}
	if len(list) != 3 || list[0] != float64(1) || list[1] != float64(2) || list[2] != float64(3) {
		t.Errorf("list: got %v (types: %T,%T,%T)", list, list[0], list[1], list[2])
	}
}

// TestBsonQuoteMap_DeviceJSONRemount covers a realistic device.json
// fragment: regex-style keys (`/.*`, `/dev/.*`) nested inside arrays of
// single-entry maps, a top-level dotted wrapper key, and string values
// that contain `.` (file paths) which must round-trip verbatim.
//
// Key points exercised:
//
//   - Top-level key `device.json` → `deviceＮjson` (every depth is
//     escaped, not just nested keys).
//   - Regex keys inside arrays: `/.*` → `/Ｎ*`, `/dev/.*` → `/dev/Ｎ*`.
//   - Value-side `.` preservation: the `path` value
//     `/storage/dm-crypt-files/.../caam.img,...` keeps its dots.
//   - `primitive.A` arrays containing `primitive.M` maps are walked.
//   - Full round-trip: unquote(quote(raw)) == raw.
func TestBsonQuoteMap_DeviceJSONRemount(t *testing.T) {
	raw := map[string]interface{}{
		"device.json": map[string]interface{}{
			"disks": []interface{}{
				map[string]interface{}{
					"name": "dm-bsh-secrets",
					"path": "/storage/dm-crypt-files/dm-bsh-secrets/caam.img,2,caam_key-bsh_secrets",
					"type": "dm-crypt-caam",
				},
			},
			"disks_v2": []interface{}{
				map[string]interface{}{
					"format":            "swap",
					"name":              "zram-swap-1",
					"provision":         "zram",
					"provision_options": "disksize=70M",
					"type":              "swap-disk",
				},
			},
			"groups": []interface{}{
				map[string]interface{}{
					"description":    "System containers",
					"name":           "System",
					"restart_policy": "system",
				},
			},
			"remount": map[string]interface{}{
				"default": []interface{}{
					map[string]interface{}{"/.*": "nosuid,noexec,noatime,nodev"},
					map[string]interface{}{"/dev": "dev"},
					map[string]interface{}{"/dev/.*": "dev"},
					map[string]interface{}{"/": "ro"},
				},
				"dev": []interface{}{
					map[string]interface{}{"/.*": "nosuid,exec,noatime,nodev"},
					map[string]interface{}{"/dev": "dev"},
					map[string]interface{}{"/dev/.*": "dev"},
					map[string]interface{}{"/": "rw"},
				},
			},
			"volumes": map[string]interface{}{
				"pv--devmeta": map[string]interface{}{
					"persistence": "boot",
				},
				"pv--phconfig": map[string]interface{}{
					"disk":        "dm-bsh-secrets",
					"persistence": "permanent",
				},
				"pv--usrmeta": map[string]interface{}{
					"disk":        "dm-bsh-secrets",
					"persistence": "permanent",
				},
			},
		},
	}

	wantQuoted := map[string]interface{}{
		"deviceＮjson": map[string]interface{}{
			"disks": []interface{}{
				map[string]interface{}{
					"name": "dm-bsh-secrets",
					"path": "/storage/dm-crypt-files/dm-bsh-secrets/caam.img,2,caam_key-bsh_secrets",
					"type": "dm-crypt-caam",
				},
			},
			"disks_v2": []interface{}{
				map[string]interface{}{
					"format":            "swap",
					"name":              "zram-swap-1",
					"provision":         "zram",
					"provision_options": "disksize=70M",
					"type":              "swap-disk",
				},
			},
			"groups": []interface{}{
				map[string]interface{}{
					"description":    "System containers",
					"name":           "System",
					"restart_policy": "system",
				},
			},
			"remount": map[string]interface{}{
				"default": []interface{}{
					map[string]interface{}{"/Ｎ*": "nosuid,noexec,noatime,nodev"},
					map[string]interface{}{"/dev": "dev"},
					map[string]interface{}{"/dev/Ｎ*": "dev"},
					map[string]interface{}{"/": "ro"},
				},
				"dev": []interface{}{
					map[string]interface{}{"/Ｎ*": "nosuid,exec,noatime,nodev"},
					map[string]interface{}{"/dev": "dev"},
					map[string]interface{}{"/dev/Ｎ*": "dev"},
					map[string]interface{}{"/": "rw"},
				},
			},
			"volumes": map[string]interface{}{
				"pv--devmeta": map[string]interface{}{
					"persistence": "boot",
				},
				"pv--phconfig": map[string]interface{}{
					"disk":        "dm-bsh-secrets",
					"persistence": "permanent",
				},
				"pv--usrmeta": map[string]interface{}{
					"disk":        "dm-bsh-secrets",
					"persistence": "permanent",
				},
			},
		},
	}

	gotQuoted := BsonQuoteMap(&raw)
	if !reflect.DeepEqual(gotQuoted, wantQuoted) {
		t.Fatalf("quote mismatch:\n got=%#v\nwant=%#v", gotQuoted, wantQuoted)
	}

	// Spot-check the value-side `.`-preservation invariant: the path
	// inside disks[0] must retain every `.` (caam.img), even though the
	// enclosing map key `device.json` was escaped.
	disks := gotQuoted["deviceＮjson"].(map[string]interface{})["disks"].([]interface{})
	gotPath := disks[0].(map[string]interface{})["path"]
	wantPath := "/storage/dm-crypt-files/dm-bsh-secrets/caam.img,2,caam_key-bsh_secrets"
	if gotPath != wantPath {
		t.Errorf("value `.`-preservation failed for disks[0].path:\n got=%q\nwant=%q", gotPath, wantPath)
	}

	// Explicit element-by-element assertions on remount.default: each
	// single-entry map inside the array must have the expected (escaped)
	// key and the exact value. This catches regressions where the array
	// walker silently drops or reshuffles elements even if the top-level
	// DeepEqual succeeds for some other reason.
	remount := gotQuoted["deviceＮjson"].(map[string]interface{})["remount"].(map[string]interface{})
	defaultArr, ok := remount["default"].([]interface{})
	if !ok {
		t.Fatalf("remount.default not []interface{}: %T", remount["default"])
	}
	wantDefault := []struct {
		key string
		val string
	}{
		{"/Ｎ*", "nosuid,noexec,noatime,nodev"},
		{"/dev", "dev"},
		{"/dev/Ｎ*", "dev"},
		{"/", "ro"},
	}
	if len(defaultArr) != len(wantDefault) {
		t.Fatalf("remount.default length: got %d, want %d", len(defaultArr), len(wantDefault))
	}
	for i, w := range wantDefault {
		entry, ok := defaultArr[i].(map[string]interface{})
		if !ok {
			t.Errorf("remount.default[%d] not a map: %T", i, defaultArr[i])
			continue
		}
		if len(entry) != 1 {
			t.Errorf("remount.default[%d] expected single-entry map, got %d keys: %v", i, len(entry), keysOf(entry))
			continue
		}
		gotV, ok := entry[w.key]
		if !ok {
			t.Errorf("remount.default[%d] missing key %q; got keys=%v", i, w.key, keysOf(entry))
			continue
		}
		if gotV != w.val {
			t.Errorf("remount.default[%d][%q]: got %q, want %q", i, w.key, gotV, w.val)
		}
	}

	// Round-trip: unquote must reverse to the original raw form exactly.
	back := BsonUnquoteMap(&gotQuoted)
	if !reflect.DeepEqual(back, raw) {
		t.Fatalf("round-trip mismatch:\n got=%#v\nwant=%#v", back, raw)
	}
}

// keysOf makes failure messages actionable — seeing the actual keys
// saves a round of log-sifting.
func keysOf(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
