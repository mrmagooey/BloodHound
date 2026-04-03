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
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteReadMessage(t *testing.T) {
	original := []byte("hello bolt protocol")
	var buf bytes.Buffer

	if err := WriteMessage(&buf, original); err != nil {
		t.Fatalf("write message: %v", err)
	}

	result, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Fatalf("expected %q, got %q", original, result)
	}
}

func TestWriteReadEmptyMessage(t *testing.T) {
	var buf bytes.Buffer

	if err := WriteMessage(&buf, []byte{}); err != nil {
		t.Fatalf("write empty message: %v", err)
	}

	// Empty message should just be the end-of-message marker (0x00 0x00)
	if buf.Len() != 2 {
		t.Fatalf("expected 2 bytes for empty message, got %d", buf.Len())
	}

	result, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read empty message: %v", err)
	}

	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d bytes", len(result))
	}
}

func TestWriteReadLargeMessage(t *testing.T) {
	// Create a message larger than maxChunkSize to force multiple chunks
	original := bytes.Repeat([]byte{0xAB}, maxChunkSize+100)
	var buf bytes.Buffer

	if err := WriteMessage(&buf, original); err != nil {
		t.Fatalf("write large message: %v", err)
	}

	result, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read large message: %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Fatalf("large message mismatch: expected %d bytes, got %d bytes", len(original), len(result))
	}
}

func TestMultipleMessages(t *testing.T) {
	messages := [][]byte{
		[]byte("first"),
		[]byte("second"),
		[]byte("third"),
	}

	var buf bytes.Buffer
	for _, msg := range messages {
		if err := WriteMessage(&buf, msg); err != nil {
			t.Fatalf("write message: %v", err)
		}
	}

	for i, expected := range messages {
		result, err := ReadMessage(&buf)
		if err != nil {
			t.Fatalf("read message %d: %v", i, err)
		}
		if !bytes.Equal(result, expected) {
			t.Fatalf("message %d mismatch: expected %q, got %q", i, expected, result)
		}
	}
}

func TestChunkFormat(t *testing.T) {
	data := []byte("test")
	var buf bytes.Buffer

	if err := WriteMessage(&buf, data); err != nil {
		t.Fatalf("write message: %v", err)
	}

	raw := buf.Bytes()

	// Should be: [00 04] [t e s t] [00 00]
	if len(raw) != 2+4+2 {
		t.Fatalf("expected 8 bytes, got %d: %x", len(raw), raw)
	}

	// Check chunk size header
	chunkSize := binary.BigEndian.Uint16(raw[0:2])
	if chunkSize != 4 {
		t.Fatalf("expected chunk size 4, got %d", chunkSize)
	}

	// Check chunk data
	if !bytes.Equal(raw[2:6], data) {
		t.Fatalf("expected data %q, got %q", data, raw[2:6])
	}

	// Check end-of-message marker
	if raw[6] != 0x00 || raw[7] != 0x00 {
		t.Fatalf("expected 00 00 terminator, got %x %x", raw[6], raw[7])
	}
}

func TestReadMessageEOF(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error reading from empty buffer")
	}
}
