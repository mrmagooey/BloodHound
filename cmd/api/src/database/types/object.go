// Copyright 2023 Specter Ops, Inc.
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

package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type JSONBObject struct {
	scannedBytes []byte
	Object       any
}

func NewJSONBObject(object any) (JSONBObject, error) {
	bytes, err := json.Marshal(object)
	if err != nil {
		return JSONBObject{}, fmt.Errorf("error marshaling scannedBytes for JSONBObject: %w", err)
	}

	return JSONBObject{
		scannedBytes: bytes,
		Object:       object,
	}, nil
}

// Scan parses the input value (expected to be JSON) to []byte and then attempts to unmarshal it into the receiver
func (s *JSONBObject) Scan(value any) error {
	switch v := value.(type) {
	case []byte:
		s.scannedBytes = v
	case string:
		s.scannedBytes = []byte(v)
	default:
		return fmt.Errorf("expected JSONB type of []byte or string but received %T", value)
	}
	return nil
}

// Map maps the value of the JSON blob onto the given interface
func (s *JSONBObject) Map(target any) error {
	if len(s.scannedBytes) == 0 {
		if s.Object == nil {
			return errors.New("JSONObject is nil")
		}

		if content, err := json.Marshal(s.Object); err != nil {
			return err
		} else {
			s.scannedBytes = content
		}
	}

	return json.Unmarshal(s.scannedBytes, target)
}

// The raw byte slice is stored in the object, so only return that part of the object and let
// the json lib take over from there. This prevents the value from being returned under the
// Object attribute.
func (s *JSONBObject) MarshalJSON() ([]byte, error) {
	return s.scannedBytes, nil
}

// Override default unmarshal behavior to save raw data in scannedBytes and to put the
// unmarshalled value directly into Object. This works around the lack of Object
// attribute in the displayed JSON.
func (s *JSONBObject) UnmarshalJSON(data []byte) error {
	var object interface{}

	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("error unmarshaling data for JSONBObject: %w", err)
	} else {
		s.Object = object
		s.scannedBytes = data

		return nil
	}
}

// Value returns the json-marshaled value of the receiver
func (s JSONBObject) Value() (driver.Value, error) {
	return json.Marshal(s.Object)
}

// GormDBDataType returns JSONB for postgres, TEXT for sqlite
func (s JSONBObject) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case "postgres":
		return "JSONB"
	case "sqlite":
		return "text"
	default:
		panic(fmt.Sprintf("Unsupported database dialect for JSON datatype: %s", db.Name()))
	}
}

type JSONUntypedObject map[string]any

// Scan parses the input value (expected to be JSON) to []byte and then attempts to unmarshal it into the receiver
func (s *JSONUntypedObject) Scan(value any) error {
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("failed to unmarshal JSONB value: %v", value)
	}
	return json.Unmarshal(b, s)
}

// Value returns the json-marshaled value of the receiver
func (s JSONUntypedObject) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// GormDBDataType returns JSONB for postgres, TEXT for sqlite
func (s JSONUntypedObject) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case "postgres":
		return "JSONB"
	case "sqlite":
		return "text"
	default:
		panic(fmt.Sprintf("Unsupported database dialect for JSON datatype: %s", db.Name()))
	}
}

type JSONBBoolObject map[string]bool

// Scan parses the input value (expected to be JSON) to []byte and then attempts to unmarshal it into the receiver
func (s *JSONBBoolObject) Scan(value any) error {
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("failed to unmarshal JSONB value: %v", value)
	}
	return json.Unmarshal(b, s)
}

// Value returns the json-marshaled value of the receiver
func (s JSONBBoolObject) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// GormDBDataType returns JSONB for postgres, TEXT for sqlite
func (s JSONBBoolObject) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case "postgres":
		return "JSONB"
	case "sqlite":
		return "text"
	default:
		panic(fmt.Sprintf("Unsupported database dialect for JSON datatype: %s", db.Name()))
	}
}

// StringArray is a []string type that works with both PostgreSQL (text[]) and SQLite (TEXT as JSON).
// On PostgreSQL it reads/writes native text[] arrays.
// On SQLite it reads/writes JSON-encoded string arrays.
type StringArray []string

// Scan implements sql.Scanner. Accepts:
//   - []byte in PostgreSQL array format {a,b,c} or JSON ["a","b","c"]
//   - string in either format
//   - nil
func (s *StringArray) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}

	var raw string
	switch v := value.(type) {
	case []byte:
		raw = string(v)
	case string:
		raw = v
	default:
		return fmt.Errorf("StringArray.Scan: unsupported type %T", value)
	}

	if raw == "" || raw == "{}" || raw == "[]" {
		*s = nil
		return nil
	}

	// Try JSON array format first (SQLite path)
	if raw[0] == '[' {
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return fmt.Errorf("StringArray.Scan: JSON unmarshal: %w", err)
		}
		*s = arr
		return nil
	}

	// PostgreSQL array format: {a,b,c}
	if raw[0] == '{' && raw[len(raw)-1] == '}' {
		inner := raw[1 : len(raw)-1]
		if inner == "" {
			*s = nil
			return nil
		}
		// Simple split — does not handle quoted elements with commas
		parts := splitPGArray(inner)
		*s = parts
		return nil
	}

	// Fallback: treat as single element
	*s = StringArray{raw}
	return nil
}

// splitPGArray splits the inner content of a PostgreSQL array literal,
// handling double-quoted elements that may contain commas.
func splitPGArray(s string) []string {
	var result []string
	var current []byte
	inQuote := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			current = append(current, c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == ',' && !inQuote {
			result = append(result, string(current))
			current = current[:0]
			continue
		}
		current = append(current, c)
	}
	result = append(result, string(current))
	return result
}

// Value implements driver.Valuer. Produces JSON for storage.
func (s StringArray) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// GormDBDataType returns the appropriate column type per dialect.
func (s StringArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case "postgres":
		return "text[]"
	case "sqlite":
		return "text"
	default:
		return "text"
	}
}
