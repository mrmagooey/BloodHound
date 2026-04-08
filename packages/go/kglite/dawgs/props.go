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
)

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

	params := make(map[string]any, len(props))
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := props[k]
		paramKey := prefix + sanitizeKey(k)
		params[paramKey] = scalarize(v)
		parts = append(parts, fmt.Sprintf("%s: $%s", k, paramKey))
	}

	return "{" + strings.Join(parts, ", ") + "}", params
}

// setClause builds a `SET var.key = $param, ...` fragment (without the SET
// keyword) and returns the associated flat params. Useful for updating all
// properties on a node or relationship variable.
func setClause(varName, prefix string, props map[string]any) (string, map[string]any) {
	if len(props) == 0 {
		return "", map[string]any{}
	}

	params := make(map[string]any, len(props))
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
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

	return strings.Join(parts, ", "), params
}

// mergeParams merges multiple param maps into one. Later maps win on collision.
func mergeParams(maps ...map[string]any) map[string]any {
	out := make(map[string]any)
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
func sanitizeKey(k string) string {
	var b strings.Builder
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
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
