// Copyright 2024 Specter Ops, Inc.
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

package bolt

import (
	"math"
	"strings"
	"testing"
)

func TestEncodeDecodeNull(t *testing.T) {
	enc := NewEncoder()
	if err := enc.Encode(nil); err != nil {
		t.Fatalf("encode nil: %v", err)
	}
	data := enc.Bytes()
	if len(data) != 1 || data[0] != markerNull {
		t.Fatalf("expected [0xC0], got %x", data)
	}
	dec := NewDecoder(data)
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode nil: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}
}

func TestEncodeDecodeBool(t *testing.T) {
	for _, b := range []bool{true, false} {
		enc := NewEncoder()
		if err := enc.Encode(b); err != nil {
			t.Fatalf("encode bool %v: %v", b, err)
		}
		dec := NewDecoder(enc.Bytes())
		val, err := dec.Decode()
		if err != nil {
			t.Fatalf("decode bool: %v", err)
		}
		if val != b {
			t.Fatalf("expected %v, got %v", b, val)
		}
	}
}

func TestEncodeDecodeIntegers(t *testing.T) {
	cases := []int64{
		0, 1, -1, 7, -16, 127,
		-17, -128, 128, 255,
		-129, -32768, 32767,
		-32769, math.MinInt32, math.MaxInt32,
		math.MinInt64, math.MaxInt64,
	}

	for _, tc := range cases {
		enc := NewEncoder()
		if err := enc.Encode(tc); err != nil {
			t.Fatalf("encode int %d: %v", tc, err)
		}
		dec := NewDecoder(enc.Bytes())
		val, err := dec.Decode()
		if err != nil {
			t.Fatalf("decode int %d: %v", tc, err)
		}
		intVal, ok := val.(int64)
		if !ok {
			t.Fatalf("expected int64 for %d, got %T", tc, val)
		}
		if intVal != tc {
			t.Fatalf("expected %d, got %d", tc, intVal)
		}
	}
}

func TestEncodeDecodeFloat64(t *testing.T) {
	cases := []float64{0.0, 1.5, -1.5, math.Pi, math.MaxFloat64, math.SmallestNonzeroFloat64}

	for _, tc := range cases {
		enc := NewEncoder()
		if err := enc.Encode(tc); err != nil {
			t.Fatalf("encode float %v: %v", tc, err)
		}
		dec := NewDecoder(enc.Bytes())
		val, err := dec.Decode()
		if err != nil {
			t.Fatalf("decode float: %v", err)
		}
		fVal, ok := val.(float64)
		if !ok {
			t.Fatalf("expected float64, got %T", val)
		}
		if fVal != tc {
			t.Fatalf("expected %v, got %v", tc, fVal)
		}
	}
}

func TestEncodeDecodeString(t *testing.T) {
	cases := []string{
		"",
		"hello",
		"hello world!!!", // 14 chars (tiny)
		strings.Repeat("a", 16),
		strings.Repeat("b", 256),
		strings.Repeat("c", 65536),
	}

	for _, tc := range cases {
		enc := NewEncoder()
		if err := enc.Encode(tc); err != nil {
			t.Fatalf("encode string len=%d: %v", len(tc), err)
		}
		dec := NewDecoder(enc.Bytes())
		val, err := dec.Decode()
		if err != nil {
			t.Fatalf("decode string len=%d: %v", len(tc), err)
		}
		sVal, ok := val.(string)
		if !ok {
			t.Fatalf("expected string, got %T", val)
		}
		if sVal != tc {
			t.Fatalf("string mismatch for len=%d", len(tc))
		}
	}
}

func TestEncodeDecodeList(t *testing.T) {
	list := []interface{}{int64(1), "two", true, nil}
	enc := NewEncoder()
	if err := enc.Encode(list); err != nil {
		t.Fatalf("encode list: %v", err)
	}
	dec := NewDecoder(enc.Bytes())
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode list: %v", err)
	}
	result, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", val)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}
	if result[0] != int64(1) {
		t.Fatalf("expected 1, got %v", result[0])
	}
	if result[1] != "two" {
		t.Fatalf("expected 'two', got %v", result[1])
	}
	if result[2] != true {
		t.Fatalf("expected true, got %v", result[2])
	}
	if result[3] != nil {
		t.Fatalf("expected nil, got %v", result[3])
	}
}

func TestEncodeDecodeMap(t *testing.T) {
	m := map[string]interface{}{
		"name": "test",
		"age":  int64(42),
	}
	enc := NewEncoder()
	if err := enc.Encode(m); err != nil {
		t.Fatalf("encode map: %v", err)
	}
	dec := NewDecoder(enc.Bytes())
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode map: %v", err)
	}
	result, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if result["name"] != "test" {
		t.Fatalf("expected name=test, got %v", result["name"])
	}
	if result["age"] != int64(42) {
		t.Fatalf("expected age=42, got %v", result["age"])
	}
}

func TestEncodeDecodeStructure(t *testing.T) {
	s := Structure{
		Tag: 0x01,
		Fields: []interface{}{
			map[string]interface{}{
				"user_agent": "test-client/1.0",
			},
		},
	}
	enc := NewEncoder()
	if err := enc.Encode(s); err != nil {
		t.Fatalf("encode structure: %v", err)
	}
	dec := NewDecoder(enc.Bytes())
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode structure: %v", err)
	}
	result, ok := val.(Structure)
	if !ok {
		t.Fatalf("expected Structure, got %T", val)
	}
	if result.Tag != 0x01 {
		t.Fatalf("expected tag 0x01, got 0x%02X", result.Tag)
	}
	if len(result.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(result.Fields))
	}
	m, ok := result.Fields[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map field, got %T", result.Fields[0])
	}
	if m["user_agent"] != "test-client/1.0" {
		t.Fatalf("expected user_agent=test-client/1.0, got %v", m["user_agent"])
	}
}

func TestEncodeDecodeEmptyCollections(t *testing.T) {
	// Empty list
	enc := NewEncoder()
	if err := enc.Encode([]interface{}{}); err != nil {
		t.Fatalf("encode empty list: %v", err)
	}
	dec := NewDecoder(enc.Bytes())
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode empty list: %v", err)
	}
	list, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected list, got %T", val)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d items", len(list))
	}

	// Empty map
	enc = NewEncoder()
	if err := enc.Encode(map[string]interface{}{}); err != nil {
		t.Fatalf("encode empty map: %v", err)
	}
	dec = NewDecoder(enc.Bytes())
	val, err = dec.Decode()
	if err != nil {
		t.Fatalf("decode empty map: %v", err)
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d items", len(m))
	}
}

func TestTinyIntRange(t *testing.T) {
	// Tiny int range: -16 to 127 should encode as single byte
	for i := int64(-16); i <= 127; i++ {
		enc := NewEncoder()
		if err := enc.Encode(i); err != nil {
			t.Fatalf("encode tiny int %d: %v", i, err)
		}
		data := enc.Bytes()
		if len(data) != 1 {
			t.Fatalf("expected 1 byte for tiny int %d, got %d bytes", i, len(data))
		}
	}

	// -17 should NOT be tiny int
	enc := NewEncoder()
	if err := enc.Encode(int64(-17)); err != nil {
		t.Fatalf("encode -17: %v", err)
	}
	data := enc.Bytes()
	if len(data) == 1 {
		t.Fatal("expected -17 to need more than 1 byte")
	}
}

func TestNestedStructures(t *testing.T) {
	nested := map[string]interface{}{
		"list": []interface{}{
			int64(1),
			map[string]interface{}{
				"inner": "value",
			},
		},
	}
	enc := NewEncoder()
	if err := enc.Encode(nested); err != nil {
		t.Fatalf("encode nested: %v", err)
	}
	dec := NewDecoder(enc.Bytes())
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode nested: %v", err)
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	list, ok := m["list"].([]interface{})
	if !ok {
		t.Fatalf("expected list, got %T", m["list"])
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list))
	}
	inner, ok := list[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inner map, got %T", list[1])
	}
	if inner["inner"] != "value" {
		t.Fatalf("expected inner=value, got %v", inner["inner"])
	}
}
