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
	"fmt"
	"math"
)

// PackStream encoder/decoder for the Neo4j Bolt protocol.
// Implements the PackStream v1 binary serialization format.

// PackStream marker bytes
const (
	markerTinyString  byte = 0x80
	markerTinyList    byte = 0x90
	markerTinyMap     byte = 0xA0
	markerTinyStruct  byte = 0xB0
	markerNull        byte = 0xC0
	markerFloat64     byte = 0xC1
	markerFalse       byte = 0xC2
	markerTrue        byte = 0xC3
	markerInt8        byte = 0xC8
	markerInt16       byte = 0xC9
	markerInt32       byte = 0xCA
	markerInt64       byte = 0xCB
	markerString8     byte = 0xD0
	markerString16    byte = 0xD1
	markerString32    byte = 0xD2
	markerList8       byte = 0xD4
	markerList16      byte = 0xD5
	markerList32      byte = 0xD6
	markerMap8        byte = 0xD8
	markerMap16       byte = 0xD9
	markerMap32       byte = 0xDA
	markerStruct8     byte = 0xDC
	markerStruct16    byte = 0xDD
)

// Structure tags for graph types
const (
	tagNode         byte = 0x4E
	tagRelationship byte = 0x52
	tagPath         byte = 0x50
)

// Encoder writes Go values into PackStream binary format.
type Encoder struct {
	buf bytes.Buffer
}

// NewEncoder creates a new PackStream encoder.
func NewEncoder() *Encoder {
	return &Encoder{}
}

// Bytes returns the encoded bytes and resets the encoder.
func (e *Encoder) Bytes() []byte {
	b := make([]byte, e.buf.Len())
	copy(b, e.buf.Bytes())
	e.buf.Reset()
	return b
}

// Encode writes a Go value in PackStream format.
func (e *Encoder) Encode(v interface{}) error {
	switch val := v.(type) {
	case nil:
		e.buf.WriteByte(markerNull)
	case bool:
		if val {
			e.buf.WriteByte(markerTrue)
		} else {
			e.buf.WriteByte(markerFalse)
		}
	case int:
		return e.encodeInt(int64(val))
	case int8:
		return e.encodeInt(int64(val))
	case int16:
		return e.encodeInt(int64(val))
	case int32:
		return e.encodeInt(int64(val))
	case int64:
		return e.encodeInt(val)
	case uint:
		return e.encodeInt(int64(val))
	case uint8:
		return e.encodeInt(int64(val))
	case uint16:
		return e.encodeInt(int64(val))
	case uint32:
		return e.encodeInt(int64(val))
	case uint64:
		return e.encodeInt(int64(val))
	case float64:
		e.buf.WriteByte(markerFloat64)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(val))
		e.buf.Write(buf[:])
	case float32:
		e.buf.WriteByte(markerFloat64)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(float64(val)))
		e.buf.Write(buf[:])
	case string:
		return e.encodeString(val)
	case []any:
		return e.encodeList(val)
	case map[string]any:
		return e.encodeMap(val)
	case Structure:
		return e.encodeStructure(val)
	default:
		return fmt.Errorf("packstream: unsupported type %T", v)
	}
	return nil
}

func (e *Encoder) encodeInt(v int64) error {
	switch {
	case v >= -16 && v <= 127:
		// Tiny int: single byte
		e.buf.WriteByte(byte(v))
	case v >= math.MinInt8 && v <= math.MaxInt8:
		e.buf.WriteByte(markerInt8)
		e.buf.WriteByte(byte(v))
	case v >= math.MinInt16 && v <= math.MaxInt16:
		e.buf.WriteByte(markerInt16)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(v))
		e.buf.Write(buf[:])
	case v >= math.MinInt32 && v <= math.MaxInt32:
		e.buf.WriteByte(markerInt32)
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(v))
		e.buf.Write(buf[:])
	default:
		e.buf.WriteByte(markerInt64)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(v))
		e.buf.Write(buf[:])
	}
	return nil
}

func (e *Encoder) encodeString(s string) error {
	b := []byte(s)
	n := len(b)
	switch {
	case n < 16:
		e.buf.WriteByte(markerTinyString | byte(n))
	case n <= 0xFF:
		e.buf.WriteByte(markerString8)
		e.buf.WriteByte(byte(n))
	case n <= 0xFFFF:
		e.buf.WriteByte(markerString16)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(n))
		e.buf.Write(buf[:])
	default:
		e.buf.WriteByte(markerString32)
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(n))
		e.buf.Write(buf[:])
	}
	e.buf.Write(b)
	return nil
}

func (e *Encoder) encodeList(list []interface{}) error {
	n := len(list)
	switch {
	case n < 16:
		e.buf.WriteByte(markerTinyList | byte(n))
	case n <= 0xFF:
		e.buf.WriteByte(markerList8)
		e.buf.WriteByte(byte(n))
	case n <= 0xFFFF:
		e.buf.WriteByte(markerList16)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(n))
		e.buf.Write(buf[:])
	default:
		e.buf.WriteByte(markerList32)
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(n))
		e.buf.Write(buf[:])
	}
	for _, item := range list {
		if err := e.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeMap(m map[string]interface{}) error {
	n := len(m)
	switch {
	case n < 16:
		e.buf.WriteByte(markerTinyMap | byte(n))
	case n <= 0xFF:
		e.buf.WriteByte(markerMap8)
		e.buf.WriteByte(byte(n))
	case n <= 0xFFFF:
		e.buf.WriteByte(markerMap16)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(n))
		e.buf.Write(buf[:])
	default:
		e.buf.WriteByte(markerMap32)
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(n))
		e.buf.Write(buf[:])
	}
	for k, v := range m {
		if err := e.encodeString(k); err != nil {
			return err
		}
		if err := e.Encode(v); err != nil {
			return err
		}
	}
	return nil
}

// Structure represents a PackStream structure (tagged tuple).
type Structure struct {
	Tag    byte
	Fields []interface{}
}

func (e *Encoder) encodeStructure(s Structure) error {
	n := len(s.Fields)
	if n < 16 {
		e.buf.WriteByte(markerTinyStruct | byte(n))
	} else if n <= 0xFF {
		e.buf.WriteByte(markerStruct8)
		e.buf.WriteByte(byte(n))
	} else {
		e.buf.WriteByte(markerStruct16)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(n))
		e.buf.Write(buf[:])
	}
	e.buf.WriteByte(s.Tag)
	for _, f := range s.Fields {
		if err := e.Encode(f); err != nil {
			return err
		}
	}
	return nil
}

// EncodeMessage encodes a Bolt message as a PackStream structure.
func (e *Encoder) EncodeMessage(tag byte, fields ...interface{}) error {
	return e.encodeStructure(Structure{Tag: tag, Fields: fields})
}

// Decoder reads PackStream binary data and returns Go values.
type Decoder struct {
	data []byte
	pos  int
}

// NewDecoder creates a new PackStream decoder for the given data.
func NewDecoder(data []byte) *Decoder {
	return &Decoder{data: data}
}

// Decode reads the next value from the PackStream data.
func (d *Decoder) Decode() (interface{}, error) {
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("packstream: unexpected end of data")
	}

	marker := d.data[d.pos]
	d.pos++

	// Tiny int (positive): 0x00..0x7F
	if marker <= 0x7F {
		return int64(marker), nil
	}

	// Tiny string: 0x80..0x8F
	if marker >= 0x80 && marker <= 0x8F {
		n := int(marker & 0x0F)
		return d.readStringN(n)
	}

	// Tiny list: 0x90..0x9F
	if marker >= 0x90 && marker <= 0x9F {
		n := int(marker & 0x0F)
		return d.readListN(n)
	}

	// Tiny map: 0xA0..0xAF
	if marker >= 0xA0 && marker <= 0xAF {
		n := int(marker & 0x0F)
		return d.readMapN(n)
	}

	// Tiny struct: 0xB0..0xBF
	if marker >= 0xB0 && marker <= 0xBF {
		n := int(marker & 0x0F)
		return d.readStructN(n)
	}

	// Negative tiny int: 0xF0..0xFF
	if marker >= 0xF0 {
		return int64(int8(marker)), nil
	}

	switch marker {
	case markerNull:
		return nil, nil
	case markerTrue:
		return true, nil
	case markerFalse:
		return false, nil
	case markerFloat64:
		return d.readFloat64()
	case markerInt8:
		if d.pos >= len(d.data) {
			return nil, fmt.Errorf("packstream: unexpected end of data reading int8")
		}
		v := int64(int8(d.data[d.pos]))
		d.pos++
		return v, nil
	case markerInt16:
		if d.pos+2 > len(d.data) {
			return nil, fmt.Errorf("packstream: unexpected end of data reading int16")
		}
		v := int64(int16(binary.BigEndian.Uint16(d.data[d.pos:])))
		d.pos += 2
		return v, nil
	case markerInt32:
		if d.pos+4 > len(d.data) {
			return nil, fmt.Errorf("packstream: unexpected end of data reading int32")
		}
		v := int64(int32(binary.BigEndian.Uint32(d.data[d.pos:])))
		d.pos += 4
		return v, nil
	case markerInt64:
		if d.pos+8 > len(d.data) {
			return nil, fmt.Errorf("packstream: unexpected end of data reading int64")
		}
		v := int64(binary.BigEndian.Uint64(d.data[d.pos:]))
		d.pos += 8
		return v, nil
	case markerString8:
		return d.readStringSized(1)
	case markerString16:
		return d.readStringSized(2)
	case markerString32:
		return d.readStringSized(4)
	case markerList8:
		return d.readListSized(1)
	case markerList16:
		return d.readListSized(2)
	case markerList32:
		return d.readListSized(4)
	case markerMap8:
		return d.readMapSized(1)
	case markerMap16:
		return d.readMapSized(2)
	case markerMap32:
		return d.readMapSized(4)
	case markerStruct8:
		return d.readStructSized(1)
	case markerStruct16:
		return d.readStructSized(2)
	default:
		return nil, fmt.Errorf("packstream: unknown marker 0x%02X", marker)
	}
}

func (d *Decoder) readFloat64() (interface{}, error) {
	if d.pos+8 > len(d.data) {
		return nil, fmt.Errorf("packstream: unexpected end of data reading float64")
	}
	bits := binary.BigEndian.Uint64(d.data[d.pos:])
	d.pos += 8
	return math.Float64frombits(bits), nil
}

func (d *Decoder) readSize(sizeBytes int) (int, error) {
	if d.pos+sizeBytes > len(d.data) {
		return 0, fmt.Errorf("packstream: unexpected end of data reading size")
	}
	var n int
	switch sizeBytes {
	case 1:
		n = int(d.data[d.pos])
		d.pos++
	case 2:
		n = int(binary.BigEndian.Uint16(d.data[d.pos:]))
		d.pos += 2
	case 4:
		n = int(binary.BigEndian.Uint32(d.data[d.pos:]))
		d.pos += 4
	}
	return n, nil
}

func (d *Decoder) readStringN(n int) (string, error) {
	if d.pos+n > len(d.data) {
		return "", fmt.Errorf("packstream: unexpected end of data reading string of length %d", n)
	}
	s := string(d.data[d.pos : d.pos+n])
	d.pos += n
	return s, nil
}

func (d *Decoder) readStringSized(sizeBytes int) (interface{}, error) {
	n, err := d.readSize(sizeBytes)
	if err != nil {
		return nil, err
	}
	return d.readStringN(n)
}

func (d *Decoder) readListN(n int) ([]interface{}, error) {
	list := make([]interface{}, n)
	for i := 0; i < n; i++ {
		v, err := d.Decode()
		if err != nil {
			return nil, err
		}
		list[i] = v
	}
	return list, nil
}

func (d *Decoder) readListSized(sizeBytes int) (interface{}, error) {
	n, err := d.readSize(sizeBytes)
	if err != nil {
		return nil, err
	}
	return d.readListN(n)
}

func (d *Decoder) readMapN(n int) (map[string]interface{}, error) {
	m := make(map[string]interface{}, n)
	for i := 0; i < n; i++ {
		keyVal, err := d.Decode()
		if err != nil {
			return nil, err
		}
		key, ok := keyVal.(string)
		if !ok {
			return nil, fmt.Errorf("packstream: map key must be string, got %T", keyVal)
		}
		val, err := d.Decode()
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	return m, nil
}

func (d *Decoder) readMapSized(sizeBytes int) (interface{}, error) {
	n, err := d.readSize(sizeBytes)
	if err != nil {
		return nil, err
	}
	return d.readMapN(n)
}

func (d *Decoder) readStructN(n int) (Structure, error) {
	if d.pos >= len(d.data) {
		return Structure{}, fmt.Errorf("packstream: unexpected end of data reading struct tag")
	}
	tag := d.data[d.pos]
	d.pos++
	fields := make([]interface{}, n)
	for i := 0; i < n; i++ {
		v, err := d.Decode()
		if err != nil {
			return Structure{}, err
		}
		fields[i] = v
	}
	return Structure{Tag: tag, Fields: fields}, nil
}

func (d *Decoder) readStructSized(sizeBytes int) (interface{}, error) {
	n, err := d.readSize(sizeBytes)
	if err != nil {
		return nil, err
	}
	return d.readStructN(n)
}
