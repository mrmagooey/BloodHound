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
	"encoding/binary"
	"fmt"
	"io"
)

// Bolt protocol chunked transport layer.
//
// Messages are framed as a sequence of chunks:
//   [uint16 chunk_size][chunk_data bytes]...
//   [0x00 0x00] (end-of-message marker)
//
// A single message may span multiple chunks. The maximum chunk size is 65535 bytes.

const maxChunkSize = 0xFFFF

// ReadMessage reads a complete chunked Bolt message from the reader.
// It reassembles all chunks until it encounters a 0x0000 end-of-message marker.
func ReadMessage(r io.Reader) ([]byte, error) {
	var message []byte
	sizeBytes := make([]byte, 2)

	for {
		if _, err := io.ReadFull(r, sizeBytes); err != nil {
			return nil, fmt.Errorf("bolt chunking: failed to read chunk size: %w", err)
		}

		chunkSize := binary.BigEndian.Uint16(sizeBytes)
		if chunkSize == 0 {
			// End of message
			return message, nil
		}

		chunk := make([]byte, chunkSize)
		if _, err := io.ReadFull(r, chunk); err != nil {
			return nil, fmt.Errorf("bolt chunking: failed to read chunk data: %w", err)
		}

		message = append(message, chunk...)
	}
}

// WriteMessage writes a complete message as one or more chunks followed by 0x0000.
func WriteMessage(w io.Writer, data []byte) error {
	for len(data) > 0 {
		chunkSize := len(data)
		if chunkSize > maxChunkSize {
			chunkSize = maxChunkSize
		}

		var header [2]byte
		binary.BigEndian.PutUint16(header[:], uint16(chunkSize))
		if _, err := w.Write(header[:]); err != nil {
			return fmt.Errorf("bolt chunking: failed to write chunk header: %w", err)
		}
		if _, err := w.Write(data[:chunkSize]); err != nil {
			return fmt.Errorf("bolt chunking: failed to write chunk data: %w", err)
		}

		data = data[chunkSize:]
	}

	// End-of-message marker
	if _, err := w.Write([]byte{0x00, 0x00}); err != nil {
		return fmt.Errorf("bolt chunking: failed to write end-of-message marker: %w", err)
	}

	return nil
}
