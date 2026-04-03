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
	"testing"
)

func TestDecodeHelloMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgHello, map[string]interface{}{
		"user_agent": "test/1.0",
		"scheme":     "none",
	}); err != nil {
		t.Fatalf("encode hello: %v", err)
	}

	tag, msg, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if tag != msgHello {
		t.Fatalf("expected tag 0x01, got 0x%02X", tag)
	}
	hello, ok := msg.(HelloMessage)
	if !ok {
		t.Fatalf("expected HelloMessage, got %T", msg)
	}
	if hello.Extra["user_agent"] != "test/1.0" {
		t.Fatalf("expected user_agent=test/1.0, got %v", hello.Extra["user_agent"])
	}
}

func TestDecodeRunMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgRun,
		"MATCH (n) RETURN n LIMIT 10",
		map[string]interface{}{},
		map[string]interface{}{},
	); err != nil {
		t.Fatalf("encode run: %v", err)
	}

	tag, msg, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if tag != msgRun {
		t.Fatalf("expected tag 0x10, got 0x%02X", tag)
	}
	run, ok := msg.(RunMessage)
	if !ok {
		t.Fatalf("expected RunMessage, got %T", msg)
	}
	if run.Query != "MATCH (n) RETURN n LIMIT 10" {
		t.Fatalf("query mismatch: %s", run.Query)
	}
}

func TestDecodePullMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgPull, map[string]interface{}{
		"n": int64(-1),
	}); err != nil {
		t.Fatalf("encode pull: %v", err)
	}

	tag, msg, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if tag != msgPull {
		t.Fatalf("expected tag 0x3F, got 0x%02X", tag)
	}
	pull, ok := msg.(PullMessage)
	if !ok {
		t.Fatalf("expected PullMessage, got %T", msg)
	}
	if n, ok := pull.Extra["n"].(int64); !ok || n != -1 {
		t.Fatalf("expected n=-1, got %v", pull.Extra["n"])
	}
}

func TestDecodeGoodbyeMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgGoodbye); err != nil {
		t.Fatalf("encode goodbye: %v", err)
	}

	tag, _, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode goodbye: %v", err)
	}
	if tag != msgGoodbye {
		t.Fatalf("expected tag 0x02, got 0x%02X", tag)
	}
}

func TestDecodeResetMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgReset); err != nil {
		t.Fatalf("encode reset: %v", err)
	}

	tag, _, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode reset: %v", err)
	}
	if tag != msgReset {
		t.Fatalf("expected tag 0x0F, got 0x%02X", tag)
	}
}

func TestDecodeBeginMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgBegin, map[string]interface{}{
		"db": "neo4j",
	}); err != nil {
		t.Fatalf("encode begin: %v", err)
	}

	tag, msg, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode begin: %v", err)
	}
	if tag != msgBegin {
		t.Fatalf("expected tag 0x11, got 0x%02X", tag)
	}
	begin, ok := msg.(BeginMessage)
	if !ok {
		t.Fatalf("expected BeginMessage, got %T", msg)
	}
	if begin.Extra["db"] != "neo4j" {
		t.Fatalf("expected db=neo4j, got %v", begin.Extra["db"])
	}
}

func TestDecodeCommitMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgCommit); err != nil {
		t.Fatalf("encode commit: %v", err)
	}

	tag, _, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode commit: %v", err)
	}
	if tag != msgCommit {
		t.Fatalf("expected tag 0x12, got 0x%02X", tag)
	}
}

func TestDecodeRollbackMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgRollback); err != nil {
		t.Fatalf("encode rollback: %v", err)
	}

	tag, _, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode rollback: %v", err)
	}
	if tag != msgRollback {
		t.Fatalf("expected tag 0x13, got 0x%02X", tag)
	}
}

func TestDecodeDiscardMessage(t *testing.T) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgDiscard, map[string]interface{}{
		"n": int64(-1),
	}); err != nil {
		t.Fatalf("encode discard: %v", err)
	}

	tag, msg, err := DecodeClientMessage(enc.Bytes())
	if err != nil {
		t.Fatalf("decode discard: %v", err)
	}
	if tag != msgDiscard {
		t.Fatalf("expected tag 0x2F, got 0x%02X", tag)
	}
	discard, ok := msg.(DiscardMessage)
	if !ok {
		t.Fatalf("expected DiscardMessage, got %T", msg)
	}
	if n, ok := discard.Extra["n"].(int64); !ok || n != -1 {
		t.Fatalf("expected n=-1, got %v", discard.Extra["n"])
	}
}

func TestEncodeSuccessMessage(t *testing.T) {
	data, err := EncodeSuccess(map[string]interface{}{
		"server": "test/1.0",
	})
	if err != nil {
		t.Fatalf("encode success: %v", err)
	}

	dec := NewDecoder(data)
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode success: %v", err)
	}
	s, ok := val.(Structure)
	if !ok {
		t.Fatalf("expected Structure, got %T", val)
	}
	if s.Tag != msgSuccess {
		t.Fatalf("expected tag 0x70, got 0x%02X", s.Tag)
	}
}

func TestEncodeFailureMessage(t *testing.T) {
	data, err := EncodeFailure("Neo.ClientError.Statement.SyntaxError", "bad query")
	if err != nil {
		t.Fatalf("encode failure: %v", err)
	}

	dec := NewDecoder(data)
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	s, ok := val.(Structure)
	if !ok {
		t.Fatalf("expected Structure, got %T", val)
	}
	if s.Tag != msgFailure {
		t.Fatalf("expected tag 0x7F, got 0x%02X", s.Tag)
	}
	meta, ok := s.Fields[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map field, got %T", s.Fields[0])
	}
	if meta["code"] != "Neo.ClientError.Statement.SyntaxError" {
		t.Fatalf("expected syntax error code, got %v", meta["code"])
	}
}

func TestEncodeRecordMessage(t *testing.T) {
	data, err := EncodeRecord([]interface{}{"hello", int64(42)})
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}

	dec := NewDecoder(data)
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode record: %v", err)
	}
	s, ok := val.(Structure)
	if !ok {
		t.Fatalf("expected Structure, got %T", val)
	}
	if s.Tag != msgRecord {
		t.Fatalf("expected tag 0x71, got 0x%02X", s.Tag)
	}
}

func TestEncodeIgnoredMessage(t *testing.T) {
	data, err := EncodeIgnored()
	if err != nil {
		t.Fatalf("encode ignored: %v", err)
	}

	dec := NewDecoder(data)
	val, err := dec.Decode()
	if err != nil {
		t.Fatalf("decode ignored: %v", err)
	}
	s, ok := val.(Structure)
	if !ok {
		t.Fatalf("expected Structure, got %T", val)
	}
	if s.Tag != msgIgnored {
		t.Fatalf("expected tag 0x7E, got 0x%02X", s.Tag)
	}
}

func TestDecodeUnknownMessage(t *testing.T) {
	enc := NewEncoder()
	// Tag 0xFF is not a valid Bolt message
	if err := enc.EncodeMessage(0xFF, map[string]interface{}{}); err != nil {
		t.Fatalf("encode unknown: %v", err)
	}

	_, _, err := DecodeClientMessage(enc.Bytes())
	if err == nil {
		t.Fatal("expected error for unknown message tag")
	}
}
