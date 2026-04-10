// Copyright 2025 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package dawgs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// sanitizeKeyCache caches sanitized property key names. Property key names in
// BloodHound are highly repetitive (objectid, name, enabled, lastseen, etc.)
// so a package-level cache eliminates repeated strings.Builder allocations
// after the first occurrence of each key. sync.Map is used for safe concurrent
// access from parallel batch goroutines.
var sanitizeKeyCache sync.Map // map[string]string

// builderPool recycles strings.Builder instances used by sanitizeKey to avoid
// allocating a new builder on every cache-miss call.
var builderPool = sync.Pool{
	New: func() any { return new(strings.Builder) },
}

// stringsPool recycles []string slices used by propsPattern and setClause for
// sorted key and fragment accumulation. Slices are returned to the pool after
// use (truncated to zero length) so subsequent calls can reuse the backing
// array without allocating.
var stringsPool = sync.Pool{
	New: func() any { s := make([]string, 0, 16); return &s },
}

// propsPattern expands a property map into an inline Cypher property pattern
// string (e.g. `{name: $p_name, objectid: $p_objectid}`) and a flat param map
// where every value is a scalar (string, int, float, bool, or nil).
//
// Non-scalar values (slices, maps, structs) are JSON-encoded and stored as
// strings — kglite only accepts scalar Cypher parameters.
//
// prefix is prepended to each param key to avoid collisions when multiple
// property maps are combined in the same query (e.g. "id_" vs "p_").
func propsPattern(prefix string, props map[string]any) (string, map[string]any) {
	if len(props) == 0 {
		return "", map[string]any{}
	}

	// Borrow a slice for key collection from the pool.
	keysPtr := stringsPool.Get().(*[]string)
	keys := (*keysPtr)[:0]

	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	params := make(map[string]any, len(props))

	// Borrow a slice for Cypher fragment accumulation.
	partsPtr := stringsPool.Get().(*[]string)
	parts := (*partsPtr)[:0]

	for _, k := range keys {
		v := props[k]
		paramKey := prefix + sanitizeKey(k)
		params[paramKey] = scalarize(v)
		parts = append(parts, fmt.Sprintf("%s: $%s", k, paramKey))
	}

	result := "{" + strings.Join(parts, ", ") + "}"

	// Return slices to pool (truncated, backing array reused).
	*keysPtr = keys[:0]
	stringsPool.Put(keysPtr)
	*partsPtr = parts[:0]
	stringsPool.Put(partsPtr)

	return result, params
}

// setClause builds a `SET var.key = $param, ...` fragment (without the SET
// keyword) and returns the associated flat params. Useful for updating all
// properties on a node or relationship variable.
func setClause(varName, prefix string, props map[string]any) (string, map[string]any) {
	if len(props) == 0 {
		return "", map[string]any{}
	}

	// Borrow a slice for key collection from the pool.
	keysPtr := stringsPool.Get().(*[]string)
	keys := (*keysPtr)[:0]

	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	params := make(map[string]any, len(props))

	// Borrow a slice for SET fragment accumulation.
	partsPtr := stringsPool.Get().(*[]string)
	parts := (*partsPtr)[:0]

	for _, k := range keys {
		// Skip "id" — kglite uses it as the immutable node identity. Source data
		// that includes "id" in properties (e.g. GitHound SCIM exports) is always
		// redundant with the top-level objectid already stored via MERGE.
		if k == "id" {
			continue
		}
		v := props[k]
		paramKey := prefix + sanitizeKey(k)
		params[paramKey] = scalarize(v)
		parts = append(parts, fmt.Sprintf("%s.%s = $%s", varName, k, paramKey))
	}

	var result string
	if len(parts) > 0 {
		result = strings.Join(parts, ", ")
	}

	// Return slices to pool.
	*keysPtr = keys[:0]
	stringsPool.Put(keysPtr)
	*partsPtr = parts[:0]
	stringsPool.Put(partsPtr)

	return result, params
}

// mergeParams merges multiple param maps into one. Later maps win on collision.
func mergeParams(maps ...map[string]any) map[string]any {
	// Compute total size to avoid rehashing.
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	out := make(map[string]any, total)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// quoteIdent wraps a Cypher identifier in backticks so that reserved words
// (e.g. Contains, Match, Return) are accepted as label/type names.
func quoteIdent(s string) string {
	return "`" + s + "`"
}

// sanitizeKey replaces characters that are not valid in a Cypher identifier
// with underscores (kglite param names must be simple identifiers).
//
// Results are cached in sanitizeKeyCache so that repeated calls for the same
// property name (e.g. "objectid", "name", "enabled") incur only a sync.Map
// lookup instead of a full strings.Builder allocation + scan.
func sanitizeKey(k string) string {
	// Fast path: return cached result if available.
	if v, ok := sanitizeKeyCache.Load(k); ok {
		return v.(string)
	}

	// Slow path: compute and cache.
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.Grow(len(k))
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	builderPool.Put(b)

	sanitizeKeyCache.Store(k, result)
	return result
}

// scalarize converts a value to one of the scalar types kglite accepts as
// Cypher parameters: string, int64, float64, bool, or nil. Slices, maps,
// and other compound types are JSON-encoded into a string.
func scalarize(v any) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string, bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
