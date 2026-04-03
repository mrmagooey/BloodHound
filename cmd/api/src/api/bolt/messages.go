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

import "fmt"

// Bolt v4.4 message tags (client -> server)
const (
	msgHello   byte = 0x01
	msgGoodbye byte = 0x02
	msgReset   byte = 0x0F
	msgRun     byte = 0x10
	msgBegin   byte = 0x11
	msgCommit  byte = 0x12
	msgRollback byte = 0x13
	msgPull    byte = 0x3F
	msgDiscard byte = 0x2F
)

// Bolt v4.4 message tags (server -> client)
const (
	msgSuccess byte = 0x70
	msgFailure byte = 0x7F
	msgIgnored byte = 0x7E
	msgRecord  byte = 0x71
)

// Client message types

// HelloMessage represents a HELLO request from the client.
type HelloMessage struct {
	Extra map[string]interface{}
}

// RunMessage represents a RUN request.
type RunMessage struct {
	Query      string
	Parameters map[string]interface{}
	Extra      map[string]interface{}
}

// PullMessage represents a PULL request.
type PullMessage struct {
	Extra map[string]interface{} // expects "n" (count) and optionally "qid"
}

// DiscardMessage represents a DISCARD request.
type DiscardMessage struct {
	Extra map[string]interface{} // expects "n" (count) and optionally "qid"
}

// BeginMessage represents a BEGIN request.
type BeginMessage struct {
	Extra map[string]interface{} // bookmarks, mode, db, etc.
}

// DecodeClientMessage decodes raw PackStream bytes into a typed client message.
// Returns the message tag and the parsed message.
func DecodeClientMessage(data []byte) (byte, interface{}, error) {
	decoder := NewDecoder(data)
	raw, err := decoder.Decode()
	if err != nil {
		return 0, nil, fmt.Errorf("bolt message: decode error: %w", err)
	}

	s, ok := raw.(Structure)
	if !ok {
		return 0, nil, fmt.Errorf("bolt message: expected structure, got %T", raw)
	}

	switch s.Tag {
	case msgHello:
		if len(s.Fields) < 1 {
			return s.Tag, nil, fmt.Errorf("bolt message: HELLO requires 1 field, got %d", len(s.Fields))
		}
		extra, _ := s.Fields[0].(map[string]interface{})
		return s.Tag, HelloMessage{Extra: extra}, nil

	case msgGoodbye:
		return s.Tag, nil, nil

	case msgReset:
		return s.Tag, nil, nil

	case msgRun:
		if len(s.Fields) < 3 {
			return s.Tag, nil, fmt.Errorf("bolt message: RUN requires 3 fields, got %d", len(s.Fields))
		}
		query, _ := s.Fields[0].(string)
		params, _ := s.Fields[1].(map[string]interface{})
		extra, _ := s.Fields[2].(map[string]interface{})
		return s.Tag, RunMessage{Query: query, Parameters: params, Extra: extra}, nil

	case msgBegin:
		var extra map[string]interface{}
		if len(s.Fields) >= 1 {
			extra, _ = s.Fields[0].(map[string]interface{})
		}
		return s.Tag, BeginMessage{Extra: extra}, nil

	case msgCommit:
		return s.Tag, nil, nil

	case msgRollback:
		return s.Tag, nil, nil

	case msgPull:
		if len(s.Fields) < 1 {
			return s.Tag, nil, fmt.Errorf("bolt message: PULL requires 1 field, got %d", len(s.Fields))
		}
		extra, _ := s.Fields[0].(map[string]interface{})
		return s.Tag, PullMessage{Extra: extra}, nil

	case msgDiscard:
		if len(s.Fields) < 1 {
			return s.Tag, nil, fmt.Errorf("bolt message: DISCARD requires 1 field, got %d", len(s.Fields))
		}
		extra, _ := s.Fields[0].(map[string]interface{})
		return s.Tag, DiscardMessage{Extra: extra}, nil

	default:
		return s.Tag, nil, fmt.Errorf("bolt message: unknown message tag 0x%02X", s.Tag)
	}
}

// Server message encoding helpers

// EncodeSuccess encodes a SUCCESS message with the given metadata.
func EncodeSuccess(metadata map[string]interface{}) ([]byte, error) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgSuccess, metadata); err != nil {
		return nil, err
	}
	return enc.Bytes(), nil
}

// EncodeFailure encodes a FAILURE message with the given code and message.
func EncodeFailure(code, message string) ([]byte, error) {
	enc := NewEncoder()
	meta := map[string]interface{}{
		"code":    code,
		"message": message,
	}
	if err := enc.EncodeMessage(msgFailure, meta); err != nil {
		return nil, err
	}
	return enc.Bytes(), nil
}

// EncodeIgnored encodes an IGNORED message.
func EncodeIgnored() ([]byte, error) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgIgnored); err != nil {
		return nil, err
	}
	return enc.Bytes(), nil
}

// EncodeRecord encodes a RECORD message with the given field values.
func EncodeRecord(fields []interface{}) ([]byte, error) {
	enc := NewEncoder()
	if err := enc.EncodeMessage(msgRecord, fields); err != nil {
		return nil, err
	}
	return enc.Bytes(), nil
}
