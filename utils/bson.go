// Copyright 2020 Pantacor Ltd.
//

package utils

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	jsonc "github.com/gibson042/canonicaljson-go"
	"github.com/microcosm-cc/bluemonday"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Placeholder for your list of compiled regexes for various secrets
var secretRegexes []*regexp.Regexp

func init() {
	// COMPILE YOUR REGEXES HERE - THIS IS CRUCIAL
	// Example (very basic, needs to be comprehensive):
	// rsaKeyRegex, _ := regexp.Compile(`-----BEGIN RSA PRIVATE KEY-----`)
	// apiKeyRegex, _ := regexp.Compile(`(?i)(api_key|secret_key|token)[\s:=]+([a-zA-Z0-9_\-]{20,})`) // Oversimplified
	// Add many more for different secret types (JWT, AWS keys, GCP keys, etc.)
	// secretRegexes = append(secretRegexes, rsaKeyRegex, apiKeyRegex)

	// Consider patterns from resources like mazen160/secrets-patterns-db
	// Example of a more specific API key pattern (fictional)
	genericAPIKey, _ := regexp.Compile(`(?i)(?:api|secret|private)_?(?:key|token|secret)\s*[:=]\s*['"]?([a-zA-Z0-9\-_]{16,})['"]?`)
	awsAccessKeyID, _ := regexp.Compile(`AKIA[0-9A-Z]{16}`)
	awsSecretKey, _ := regexp.Compile(`(?i)aws(.{0,20})?['"][0-9a-zA-Z\/+]{40}['"]`) // From gitleaks
	sshPrivateKey, _ := regexp.Compile(`-----BEGIN ((EC|PGP|DSA|RSA|OPENSSH) )?PRIVATE KEY( BLOCK)?-----`)
	sshRSAPublicKeyRegex, _ := regexp.Compile(`^ssh-rsa`)
	// Add more patterns...
	secretRegexes = append(secretRegexes, sshRSAPublicKeyRegex, genericAPIKey, awsAccessKeyID, awsSecretKey, sshPrivateKey)
}

// isSecret heuristically determines if a string value is a secret.
// THIS IS THE FUNCTION YOU NEED TO MAKE ROBUST.
func isSecret(value string) bool {
	// 1. Check against compiled regular expressions
	for _, r := range secretRegexes {
		if r.MatchString(value) {
			return true
		}
	}

	// 2. Check for high entropy (requires an entropy calculation function)
	// For example, Shannon entropy:
	// entropy := calculateShannonEntropy(value)
	// if len(value) > 20 && entropy > 4.0 { // Adjust thresholds
	// 	return true
	// }

	// 3. Check for common password-like characteristics (length, mix of char types)
	// This is harder and more prone to false positives without context.
	// if len(value) >= 12 && regexp.MustCompile(`[a-z]`).MatchString(value) &&
	// 	regexp.MustCompile(`[A-Z]`).MatchString(value) &&
	// 	regexp.MustCompile(`[0-9]`).MatchString(value) &&
	// 	regexp.MustCompile(`[\W_]`).MatchString(value) { // \W is non-alphanumeric
	// 	// Be careful with this, it can match many non-secrets.
	// 	// Consider combining with key name hints if you relax your "no prior key knowledge" rule.
	// 	// fmt.Printf("Potential password-like string (use with caution): %s\n", value)
	// 	return true // Enable cautiously
	// }

	// 4. Check for JWTs (three Base64-URL encoded parts separated by dots)
	// jwtRegex := regexp.MustCompile(`^[A-Za-z0-9-_=]+\.[A-Za-z0-9-_=]+\.[A-Za-z0-9-_.+/=]*$`)
	// if jwtRegex.MatchString(value) {
	// 	// Further validation of JWT structure can be done here if needed
	// 	return true
	// }

	return false
}

// obfuscateValue recursively traverses the JSON structure.
func obfuscateValue(data interface{}) interface{} {
	switch val := data.(type) {
	case map[string]interface{}:
		obfuscatedMap := make(map[string]interface{})
		for k, v := range val {
			obfuscatedMap[k] = obfuscateValue(v) // Recurse
		}
		return obfuscatedMap
	case []interface{}:
		obfuscatedArray := make([]interface{}, len(val))
		for i, v := range val {
			obfuscatedArray[i] = obfuscateValue(v) // Recurse
		}
		return obfuscatedArray
	case string:
		if isSecret(val) {
			return "[REDACTED]"
		}
		return val
	default:
		// For numbers, booleans, null - return as is
		return val
	}
}

func QuoteSecrets(m *map[string]interface{}) map[string]interface{} {
	escapedMap := map[string]interface{}{}
	for k, v := range *m {
		escapedMap[k] = obfuscateValue(v)
	}

	return escapedMap
}

// BsonQuoteMap returns a deep copy of *m in Pantahub's canonical BSON
// encoding:
//
//	         KEYS                          STRING VALUES
//	.    →   U+FF2E  (Ｎ, "bson quote")    .   →   (left raw)
//	$    →   U+FFE0  (￠, "dollar quote")  $   →   U+FFE0 (￠)
//
// Rationale for the asymmetry on `.`:
//
//   - Mongo uses `.` as the dotted-path separator in queries / updates.
//     A raw `.` in a KEY is ambiguous and older mongo versions rejected
//     it outright, so keys must be escaped.
//   - String VALUES are opaque to dotted-path parsing, so `.` in a value
//     is harmless and preserved verbatim — round-tripping user data
//     (URLs, pantavisor.revision strings, etc.) untouched is the
//     contract.
//
// Rationale for escaping `$` in BOTH keys and values:
//
//   - Mongo treats `$` as the operator prefix. While only top-level
//     keys strictly need escaping, the historic Pantahub rule (and the
//     pantahub-base encoder) is "no raw `$` anywhere in a stored doc".
//     That way aggregations, $expr, query rewriting, etc. can never
//     accidentally interpret a value as an operator reference.
//   - The old BsonQuoteMap implemented this via cjson.Marshal → global
//     strings.ReplaceAll("$","￠") → cjson.Unmarshal. That achieved the
//     value-side escape correctly, but only escaped top-level KEY `.`
//     (no recursion). This implementation walks the tree properly and
//     escapes `.` in every nested key too.
//
// Invariants:
//
//   - Recursive: nested map[string]interface{} / primitive.M values are
//     walked; nested []interface{} / primitive.A are walked element-wise.
//   - Idempotent: applying this twice produces the same result
//     (quote functions are no-ops on inputs that already contain only
//     sentinel runes).
//   - Input is not mutated.
//   - Collisions: if two distinct raw keys canonicalise to the same
//     sentinel key in the same map, the first encounter wins. This is
//     rare (requires e.g. both `a.b` and `aＮb` to coexist) and is the
//     same behaviour as the hubmanager bsonfix rewriter.
//
// Returns nil when m is nil.
//
// Implementation: a two-step hybrid.
//
//  1. Tree walk that escapes `.→Ｎ` in KEYS only, at every depth.
//     Values are left untouched (including any `.` they contain —
//     URLs, pantavisor.revision, file paths must survive verbatim).
//     The walk recurses through map[string]interface{} / primitive.M
//     and []interface{} / primitive.A.
//
//  2. JSON round-trip with a global `$→￠` replaceAll on the marshalled
//     text. JSON uses no `$` as a syntactic character (and
//     encoding/json never emits `$` for `$` in any position),
//     so a blanket text replace catches every `$` in every key and
//     every string value at every depth in a single linear pass —
//     without the tree-walker having to type-switch on every leaf.
//     This matches the historical BsonQuoteMap semantics (the old
//     cjson-based implementation used the same trick) and is the
//     security-critical path: `$` MUST NEVER appear raw in a stored
//     doc, else it becomes a mongo operator-injection vector
//     (`$where`, `$function`, `$expr`).
//
// Idempotent: applying this twice produces the same output. Step 1
// is a no-op on already-escaped keys, step 2 is a no-op on already-
// escaped `$`.
func BsonQuoteMap(m *map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	dotEscaped := walkMap(*m, BsonQuote, nil)
	return jsonReplaceAll(dotEscaped, "$", "￠")
}

// BsonUnquoteMap is the inverse of BsonQuoteMap.
//
//	         KEYS                          STRING VALUES
//	Ｎ   →   .                             (verbatim — never our escape)
//	￠   →   $                             ￠   →   $
//
// Values are treated as opaque: we only reverse what BsonQuoteMap
// could have written. `Ｎ` in a value is either legitimate user data
// (a string that happens to contain U+FF2E, e.g. a rendered label of a
// quoted key) or — rarely — a pre-fix encoder leak. Either way, the
// runtime unquoter must not silently rewrite it. The hubmanager
// bsonfix scanner is the right place to surface true leaks for
// operator review.
//
// Recursive, idempotent, returns nil for nil input.
//
// Implementation: the inverse of BsonQuoteMap's hybrid.
//
//  1. JSON round-trip with a global `￠→$` replace. Reverses the
//     security encoding in one linear pass — applies to every `￠`
//     whether it was a key sentinel or an in-value escape.
//
//  2. Tree walk that reverses `Ｎ→.` in KEYS only, at every depth.
//     `Ｎ` in a VALUE is opaque user data (e.g. a pre-quoted key
//     rendered into a label) and must not be silently rewritten.
func BsonUnquoteMap(m *map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	dollarReversed := jsonReplaceAll(*m, "￠", "$")
	return walkMap(dollarReversed, BsonUnquote, nil)
}

// jsonReplaceAll marshals v to JSON, performs strings.ReplaceAll on
// the marshalled text, and unmarshals back into a fresh
// map[string]interface{}. The replace operates on the entire JSON
// stream, so it hits both KEYS and string VALUES at every depth in
// one linear pass — no tree recursion needed.
//
// Safety: this is only called with from/to drawn from
// {"$","￠","Ｎ","."}. None of those are JSON syntactic characters,
// so a blanket text replace can't corrupt the JSON structure:
//
//   - `$` and `￠` never appear as JSON delimiters.
//   - `Ｎ` is U+FF2E, always emitted as a literal multi-byte rune in
//     UTF-8 string content (encoding/json does not escape it).
//   - `.` appears inside JSON numbers (as the decimal separator)
//     but callers of jsonReplaceAll NEVER pass `.` as the `from`
//     argument — the `.→Ｎ` transform is handled by walkMap on
//     KEYS only, specifically to avoid touching `.` inside numbers
//     and inside string values.
//
// Failure modes: marshal or unmarshal errors fall back to returning
// the input map verbatim and log to stderr. The old cjson-based
// implementation did the same — continuing is strictly better than
// dropping the write, because the input is still a usable map even
// if its escape encoding is partial.
//
// Type fidelity caveat: JSON doesn't distinguish int from float, so
// integer leaves come back as float64. That matches the legacy
// cjson-based BsonQuoteMap behaviour; callers that need integer
// precision already go through typed struct fields rather than
// through map[string]interface{}.
func jsonReplaceAll(v map[string]interface{}, from, to string) map[string]interface{} {
	b, err := jsonc.Marshal(v)
	if err != nil {
		fmt.Printf("BsonQuoteMap/BsonUnquoteMap: json.Marshal failed: %v\n", err)
		return v
	}
	if strings.Contains(string(b), from) {
		b = []byte(strings.ReplaceAll(string(b), from, to))
	}
	out := map[string]interface{}{}
	if err := jsonc.Unmarshal(b, &out); err != nil {
		fmt.Printf("BsonQuoteMap/BsonUnquoteMap: json.Unmarshal failed: %v\n", err)
		return v
	}
	return out
}

// walkMap rewrites every key in `in` using keyFn and recurses into
// child maps and arrays. Emits map[string]interface{} regardless of
// whether the input was map[string]interface{} or primitive.M (the
// downstream mongo driver accepts either when the field tag is
// `bson:"..."` on a `map[string]interface{}` struct field).
//
// valFn, when non-nil, is applied to every string VALUE. In the current
// hybrid design BsonQuoteMap / BsonUnquoteMap both pass nil — value-
// side transforms are done by the JSON round-trip instead. valFn is
// kept as a parameter so the walker remains a general-purpose helper
// (tests and future call sites can pass a value function if needed).
func walkMap(in map[string]interface{}, keyFn func(string) string, valFn func(string) string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		nk := keyFn(k)
		if _, clash := out[nk]; clash {
			// Deterministic tie-break: keep the first encounter. Ranging
			// over a map is unordered in Go, so which key "wins" is
			// technically nondeterministic — that's acceptable because
			// a collision here means the input was already ambiguous
			// (two distinct raw keys canonicalising to the same
			// sentinel key). The hubmanager bsonfix scanner flags this
			// pattern separately.
			continue
		}
		out[nk] = walkValue(v, keyFn, valFn)
	}
	return out
}

// walkValue dispatches on v's dynamic type:
//
//   - Maps (map[string]interface{} / primitive.M) recurse via walkMap.
//   - Arrays ([]interface{} / primitive.A) recurse element-wise,
//     emitting []interface{} (same driver-agnostic reasoning as walkMap).
//   - Strings go through valFn if non-nil, verbatim otherwise.
//   - Any other leaf (numbers, bools, time.Time, primitive.ObjectID,
//     primitive.DateTime, nil, etc.) is returned unchanged.
func walkValue(v interface{}, keyFn func(string) string, valFn func(string) string) interface{} {
	switch node := v.(type) {
	case map[string]interface{}:
		return walkMap(node, keyFn, valFn)
	case primitive.M:
		return walkMap(map[string]interface{}(node), keyFn, valFn)
	case []interface{}:
		out := make([]interface{}, len(node))
		for i, c := range node {
			out[i] = walkValue(c, keyFn, valFn)
		}
		return out
	case primitive.A:
		out := make([]interface{}, len(node))
		for i, c := range node {
			out[i] = walkValue(c, keyFn, valFn)
		}
		return out
	case string:
		if valFn != nil {
			return valFn(node)
		}
		return node
	default:
		return v
	}
}

// BsonUnquoteAndDollar unquote a string and remove dollar signs
func BsonUnquoteAndDollar(s string) string {
	return BsonUnquote(unquoteDollar(s))
}

// BsonUnquote unquote a string
func BsonUnquote(s string) string {
	return strings.ReplaceAll(s, "\uFF2E", ".")
}

// BsonQuote quote a string
func BsonQuote(s string) string {
	return strings.ReplaceAll(s, ".", "\uFF2E")
}

func unquoteDollar(s string) string {
	return strings.ReplaceAll(s, "\uFFE0", "$")
}

// Slugify converts a string into a slug standard string
func Slugify(s string) string {
	// ReplaceAll special characters and spaces with hyphens
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '-'
	}, s)

	// Convert to lowercase
	s = strings.ToLower(s)

	// Remove multiple consecutive hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Remove leading and trailing hyphens
	s = strings.Trim(s, "-")

	return s
}

func SanitizeInput(input string) string {
	p := bluemonday.UGCPolicy() // Allows safe HTML but removes scripts
	return p.Sanitize(input)
}
