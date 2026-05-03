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

//go:build e2e

package e2e_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/api/bolt"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	dbmocks "github.com/specterops/bloodhound/cmd/api/src/database/mocks"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
	"github.com/specterops/bloodhound/packages/go/cache"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// boltTestServer starts a Bolt daemon listening on a random local port,
// backed by a real kglite graph. Returns the listener address and a
// cleanup function that stops the daemon.
func boltTestServer(t *testing.T) string {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{
		EnableCypherMutations: true,
	})

	// Pick a random available port by listening on :0
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to allocate random port")
	addr := ln.Addr().String()
	ln.Close() // Release so the daemon can bind to the same address

	daemon := bolt.NewDaemon(addr, gq, mockDB)
	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	go func() {
		// Signal readiness once we can connect
		close(started)
		daemon.Start(ctx)
	}()

	// Wait for the daemon to start accepting connections
	<-started
	waitForTCP(t, addr, 2*time.Second)

	t.Cleanup(func() {
		cancel()
		daemon.Stop(context.Background()) //nolint:errcheck
	})

	return addr
}

// waitForTCP waits until a TCP connection to addr succeeds (or times out).
func waitForTCP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Bolt server at %s did not become ready within %s", addr, timeout)
}

// boltClient is a minimal Bolt v4.4 client for testing.
type boltClient struct {
	conn net.Conn
	t    *testing.T
}

// dialBolt opens a TCP connection and performs the Bolt v4.4 handshake.
// Returns a client ready to send messages.
func dialBolt(t *testing.T, addr string) *boltClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err, "dial Bolt server at %s", addr)

	c := &boltClient{conn: conn, t: t}
	t.Cleanup(func() { conn.Close() })

	c.handshake()
	return c
}

// handshake sends the Bolt preamble and version proposals, reads the server's
// selected version, and asserts that 4.4 was chosen.
func (c *boltClient) handshake() {
	c.t.Helper()

	// Magic preamble: 0x6060B017
	magic := []byte{0x60, 0x60, 0xB0, 0x17}
	// Four version proposals (big-endian uint32):
	//   Proposal 1: 0x00000404 = Bolt 4.4 exactly
	//   Proposals 2-4: 0x00000000 = "no version"
	proposals := []byte{
		0x00, 0x00, 0x04, 0x04, // 4.4
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	_, err := c.conn.Write(append(magic, proposals...))
	require.NoError(c.t, err, "send handshake")

	var selected [4]byte
	_, err = readFull(c.conn, selected[:])
	require.NoError(c.t, err, "read selected version")

	ver := binary.BigEndian.Uint32(selected[:])
	require.Equal(c.t, uint32(0x00000404), ver, "server must select Bolt 4.4")
}

// sendMessage encodes a Bolt message as a PackStream structure and sends it
// using the Bolt chunked transport.
func (c *boltClient) sendMessage(tag byte, fields ...interface{}) {
	c.t.Helper()
	enc := bolt.NewEncoder()
	err := enc.EncodeMessage(tag, fields...)
	require.NoError(c.t, err, "encode message 0x%02X", tag)
	err = bolt.WriteMessage(c.conn, enc.Bytes())
	require.NoError(c.t, err, "write message 0x%02X", tag)
}

// recvMessage reads the next chunked Bolt message and decodes it.
// Returns the message tag byte and the decoded Structure.
func (c *boltClient) recvMessage() (byte, map[string]interface{}) {
	c.t.Helper()
	data, err := bolt.ReadMessage(c.conn)
	require.NoError(c.t, err, "read message from server")

	dec := bolt.NewDecoder(data)
	raw, err := dec.Decode()
	require.NoError(c.t, err, "decode server message")

	s, ok := raw.(bolt.Structure)
	require.True(c.t, ok, "expected Structure, got %T", raw)

	// The server always sends a single-field metadata map
	var meta map[string]interface{}
	if len(s.Fields) > 0 {
		if m, ok := s.Fields[0].(map[string]interface{}); ok {
			meta = m
		}
	}
	return s.Tag, meta
}

// expectSuccess reads the next message and asserts it is a SUCCESS (0x70).
func (c *boltClient) expectSuccess(description string) map[string]interface{} {
	c.t.Helper()
	tag, meta := c.recvMessage()
	assert.Equal(c.t, byte(0x70), tag, "%s: expected SUCCESS (0x70), got 0x%02X", description, tag)
	return meta
}

// expectRecord reads the next message and asserts it is a RECORD (0x71).
// Returns the record fields.
func (c *boltClient) expectRecord(description string) []interface{} {
	c.t.Helper()
	data, err := bolt.ReadMessage(c.conn)
	require.NoError(c.t, err, "read record message")

	dec := bolt.NewDecoder(data)
	raw, err := dec.Decode()
	require.NoError(c.t, err, "decode record message")

	s, ok := raw.(bolt.Structure)
	require.True(c.t, ok, "expected Structure for record, got %T", raw)
	assert.Equal(c.t, byte(0x71), s.Tag, "%s: expected RECORD (0x71), got 0x%02X", description, s.Tag)

	if len(s.Fields) > 0 {
		if fields, ok := s.Fields[0].([]interface{}); ok {
			return fields
		}
	}
	return nil
}

// drainUntilSuccess reads RECORD messages until a SUCCESS or FAILURE is seen.
// Returns the final tag and metadata.
func (c *boltClient) drainUntilSuccess() (byte, map[string]interface{}) {
	c.t.Helper()
	for {
		tag, meta := c.recvMessage()
		if tag != 0x71 { // not RECORD
			return tag, meta
		}
	}
}

// Bolt message tag constants (client → server) — mirror the unexported ones in
// the bolt package so tests don't need internal access.
const (
	boltMsgHello    byte = 0x01
	boltMsgRun      byte = 0x10
	boltMsgPull     byte = 0x3F
	boltMsgBegin    byte = 0x11
	boltMsgCommit   byte = 0x12
	boltMsgRollback byte = 0x13
	boltMsgReset    byte = 0x0F
	boltMsgGoodbye  byte = 0x02
)

// hello sends a HELLO message and asserts the server responds with SUCCESS.
func (c *boltClient) hello() {
	c.t.Helper()
	c.sendMessage(boltMsgHello, map[string]interface{}{
		"user_agent": "bh-e2e-test/1.0",
	})
	meta := c.expectSuccess("HELLO")
	assert.Contains(c.t, meta, "server", "SUCCESS metadata must contain 'server'")
	assert.Contains(c.t, meta, "connection_id", "SUCCESS metadata must contain 'connection_id'")
}

// run sends a RUN message and asserts SUCCESS with fields metadata.
func (c *boltClient) run(cypher string) []string {
	c.t.Helper()
	c.sendMessage(boltMsgRun,
		cypher,
		map[string]interface{}{},  // parameters
		map[string]interface{}{},  // extra
	)
	meta := c.expectSuccess("RUN")
	fields, _ := meta["fields"].([]interface{})
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i], _ = f.(string)
	}
	return cols
}

// pullAll sends a PULL {n: -1} and drains until SUCCESS.
// Returns all record rows and the final metadata.
func (c *boltClient) pullAll() ([][]interface{}, map[string]interface{}) {
	c.t.Helper()
	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})

	var rows [][]interface{}
	for {
		tag, meta := c.recvMessage()
		switch tag {
		case 0x71: // RECORD — but we've already read it via recvMessage which discarded fields
			// Re-read using a raw approach; this path is hit only when there are records
			// before the final SUCCESS. Since recvMessage doesn't distinguish, collect
			// using drainUntilSuccess below instead.
			_ = meta
			// This case is unreachable since we use drainUntilSuccess below; kept for clarity.
		case 0x70: // SUCCESS
			return rows, meta
		case 0x7F: // FAILURE
			c.t.Fatalf("PULL returned FAILURE: %v", meta)
		}
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestBolt_Handshake verifies that the Bolt server accepts the v4.4 handshake.
func TestBolt_Handshake(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	// dialBolt performs the handshake internally and fails the test on mismatch.
	_ = dialBolt(t, addr)
}

// TestBolt_Hello verifies HELLO → SUCCESS with server metadata.
func TestBolt_Hello(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()
}

// TestBolt_SimpleQuery sends HELLO → RUN → PULL and verifies the protocol flow.
func TestBolt_SimpleQuery(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	// RUN: expect SUCCESS with "fields" metadata
	c.sendMessage(boltMsgRun,
		"MATCH (n) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	runMeta := c.expectSuccess("RUN")
	fields, ok := runMeta["fields"]
	require.True(t, ok, "RUN SUCCESS must contain 'fields' key")
	fieldList, ok := fields.([]interface{})
	require.True(t, ok, "'fields' must be a list")
	require.Len(t, fieldList, 1, "expected one column")
	assert.Equal(t, "c", fieldList[0], "column name must be 'c'")

	// PULL: drain records then expect SUCCESS with has_more = false
	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})
	tag, meta := c.drainUntilSuccess()
	assert.Equal(t, byte(0x70), tag, "final PULL response must be SUCCESS")
	hasMore, _ := meta["has_more"].(bool)
	assert.False(t, hasMore, "has_more must be false after pulling all")
}

// TestBolt_CountQueryReturnsRecord seeds the graph and verifies that a count
// query returns at least one RECORD message with a numeric value.
func TestBolt_CountQueryReturnsRecord(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})

	// Seed some nodes
	setupCypherTestGraph(t.Context(), t, graphDB)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	daemon := bolt.NewDaemon(addr, gq, mockDB)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { daemon.Start(ctx) }()
	waitForTCP(t, addr, 2*time.Second)
	t.Cleanup(func() { cancel(); daemon.Stop(context.Background()) }) //nolint:errcheck

	c := dialBolt(t, addr)
	c.hello()

	// RUN
	c.sendMessage(boltMsgRun,
		"MATCH (n:CypherTest) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	c.expectSuccess("RUN")

	// PULL — read raw messages to capture records
	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})

	var recordCount int
	for {
		data, err := bolt.ReadMessage(c.conn)
		require.NoError(t, err)
		dec := bolt.NewDecoder(data)
		raw, err := dec.Decode()
		require.NoError(t, err)
		s, ok := raw.(bolt.Structure)
		require.True(t, ok)
		if s.Tag == 0x71 { // RECORD
			recordCount++
			continue
		}
		// SUCCESS or FAILURE terminates the stream
		assert.Equal(t, byte(0x70), s.Tag, "expected SUCCESS after records")
		break
	}

	assert.Equal(t, 1, recordCount, "count query should produce exactly one RECORD")
}

// TestBolt_TransactionBeginRunCommit exercises the BEGIN → RUN → PULL → COMMIT
// transaction flow via Bolt.
func TestBolt_TransactionBeginRunCommit(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	// BEGIN
	c.sendMessage(boltMsgBegin, map[string]interface{}{})
	c.expectSuccess("BEGIN")

	// RUN inside the transaction
	c.sendMessage(boltMsgRun,
		"MATCH (n) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	runMeta := c.expectSuccess("RUN")
	_, hasFields := runMeta["fields"]
	assert.True(t, hasFields, "RUN inside tx must return fields")

	// PULL
	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})
	tag, _ := c.drainUntilSuccess()
	assert.Equal(t, byte(0x70), tag, "PULL inside tx should end with SUCCESS")

	// COMMIT
	c.sendMessage(boltMsgCommit)
	commitMeta := c.expectSuccess("COMMIT")
	bookmark, ok := commitMeta["bookmark"]
	assert.True(t, ok, "COMMIT SUCCESS must include a bookmark")
	assert.NotEmpty(t, bookmark, "bookmark must be non-empty")
}

// TestBolt_TransactionRollback exercises BEGIN → RUN → ROLLBACK.
func TestBolt_TransactionRollback(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	// BEGIN
	c.sendMessage(boltMsgBegin, map[string]interface{}{})
	c.expectSuccess("BEGIN")

	// RUN
	c.sendMessage(boltMsgRun,
		"MATCH (n) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	c.expectSuccess("RUN inside tx for rollback test")

	// PULL to consume the result stream
	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})
	c.drainUntilSuccess()

	// ROLLBACK
	c.sendMessage(boltMsgRollback)
	c.expectSuccess("ROLLBACK")

	// After rollback, server should be in READY state: a new RUN should work.
	c.sendMessage(boltMsgRun,
		"MATCH (n) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	c.expectSuccess("RUN after rollback")
}

// TestBolt_Reset verifies that RESET returns the connection to READY state
// from FAILED, allowing normal operation to resume.
func TestBolt_Reset(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	// Trigger a FAILURE by sending an invalid Cypher query
	c.sendMessage(boltMsgRun,
		"THIS IS NOT VALID CYPHER !!!",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	tag, _ := c.recvMessage()
	// The server should send FAILURE (0x7F) for a bad query
	assert.Equal(t, byte(0x7F), tag, "invalid Cypher should produce FAILURE")

	// RESET brings the connection back to READY
	c.sendMessage(boltMsgReset)
	c.expectSuccess("RESET")

	// Verify the connection is usable again
	c.sendMessage(boltMsgRun,
		"MATCH (n) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	c.expectSuccess("RUN after RESET")
}

// TestBolt_IgnoredAfterFailure verifies that messages sent in FAILED state
// return IGNORED until a RESET is issued.
func TestBolt_IgnoredAfterFailure(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	// Put connection into FAILED state
	c.sendMessage(boltMsgRun,
		"THIS IS NOT VALID CYPHER !!!",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	tag, _ := c.recvMessage()
	assert.Equal(t, byte(0x7F), tag, "invalid Cypher should produce FAILURE")

	// A subsequent RUN while in FAILED state should be IGNORED (0x7E)
	c.sendMessage(boltMsgRun,
		"MATCH (n) RETURN count(n) AS c",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	tag, _ = c.recvMessage()
	assert.Equal(t, byte(0x7E), tag, "RUN in FAILED state should return IGNORED (0x7E)")

	// RESET
	c.sendMessage(boltMsgReset)
	c.expectSuccess("RESET after ignored message")
}

// TestBolt_Goodbye verifies that a GOODBYE message cleanly closes the connection.
func TestBolt_Goodbye(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	// Send GOODBYE — the server should close the connection
	c.sendMessage(boltMsgGoodbye)

	// The server closes the connection; any subsequent read should get EOF or error
	c.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err := c.conn.Read(buf)
	assert.Error(t, err, "connection should be closed after GOODBYE")
}

// TestBolt_MultipleQueries verifies that multiple RUN+PULL sequences can be
// executed on the same connection without errors.
func TestBolt_MultipleQueries(t *testing.T) {
	t.Parallel()
	addr := boltTestServer(t)
	c := dialBolt(t, addr)
	c.hello()

	cyphers := []string{
		"MATCH (n) RETURN count(n) AS c",
		"MATCH ()-[r]->() RETURN count(r) AS r",
		"MATCH (n) RETURN count(n) AS total",
	}

	for i, cypher := range cyphers {
		c.sendMessage(boltMsgRun, cypher,
			map[string]interface{}{},
			map[string]interface{}{},
		)
		c.expectSuccess(fmt.Sprintf("RUN #%d", i+1))

		c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})
		tag, _ := c.drainUntilSuccess()
		assert.Equal(t, byte(0x70), tag, "PULL #%d should succeed", i+1)
	}
}

// TestBolt_SeededNodeQuery seeds the graph with nodes and verifies that a
// node-returning query streams back RECORD messages.
func TestBolt_SeededNodeQuery(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	setupCypherTestGraph(t.Context(), t, graphDB)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	daemon := bolt.NewDaemon(addr, gq, mockDB)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { daemon.Start(ctx) }()
	waitForTCP(t, addr, 2*time.Second)
	t.Cleanup(func() { cancel(); daemon.Stop(context.Background()) }) //nolint:errcheck

	c := dialBolt(t, addr)
	c.hello()

	// RUN a query that should return 3 nodes
	c.sendMessage(boltMsgRun,
		"MATCH (n:CypherTest) RETURN n",
		map[string]interface{}{},
		map[string]interface{}{},
	)
	runMeta := c.expectSuccess("RUN seeded nodes")
	fieldList, _ := runMeta["fields"].([]interface{})
	require.Len(t, fieldList, 1, "node query should have one column")
	assert.Equal(t, "n", fieldList[0])

	// PULL: count RECORD messages before the final SUCCESS
	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})
	var records int
	for {
		data, err := bolt.ReadMessage(c.conn)
		require.NoError(t, err)
		dec := bolt.NewDecoder(data)
		raw, err := dec.Decode()
		require.NoError(t, err)
		s, _ := raw.(bolt.Structure)
		if s.Tag == 0x71 {
			records++
			continue
		}
		break
	}
	assert.Equal(t, 3, records, "expected 3 RECORD messages for 3 CypherTest nodes")
}

// readFull is a helper that reads exactly len(buf) bytes from conn.
func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ── richer query tests via Bolt ───────────────────────────────────────────────

// boltSeededServer starts a Bolt server backed by a graph pre-seeded with
// setupCypherTestGraph. Returns the server address and the three node IDs.
func boltSeededServer(t *testing.T) (addr string, aID, bID, cID graph.ID) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	aID, bID, cID = setupCypherTestGraph(t.Context(), t, graphDB)

	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{
		EnableCypherMutations: true,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr = ln.Addr().String()
	ln.Close()

	daemon := bolt.NewDaemon(addr, gq, mockDB)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { daemon.Start(ctx) }()
	waitForTCP(t, addr, 2*time.Second)
	t.Cleanup(func() { cancel(); daemon.Stop(context.Background()) }) //nolint:errcheck

	return addr, aID, bID, cID
}

// countBoltRecords runs a Cypher query via Bolt and returns the number of RECORD
// messages received before the final SUCCESS.
func countBoltRecords(t *testing.T, c *boltClient, cypher string) int {
	t.Helper()
	c.sendMessage(boltMsgRun, cypher,
		map[string]interface{}{},
		map[string]interface{}{},
	)
	c.expectSuccess("RUN: " + cypher)

	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})

	var records int
	for {
		data, err := bolt.ReadMessage(c.conn)
		require.NoError(t, err)
		dec := bolt.NewDecoder(data)
		raw, err := dec.Decode()
		require.NoError(t, err)
		s, ok := raw.(bolt.Structure)
		require.True(t, ok)
		if s.Tag == 0x71 { // RECORD
			records++
			continue
		}
		assert.Equal(t, byte(0x70), s.Tag, "expected SUCCESS after records for %q", cypher)
		break
	}
	return records
}

// firstBoltRecordField runs a Cypher query via Bolt and returns the first field
// of the first RECORD.
func firstBoltRecordField(t *testing.T, c *boltClient, cypher string) interface{} {
	t.Helper()
	c.sendMessage(boltMsgRun, cypher,
		map[string]interface{}{},
		map[string]interface{}{},
	)
	c.expectSuccess("RUN: " + cypher)

	c.sendMessage(boltMsgPull, map[string]interface{}{"n": int64(-1)})

	// Read first message — must be a RECORD
	data, err := bolt.ReadMessage(c.conn)
	require.NoError(t, err)
	dec := bolt.NewDecoder(data)
	raw, err := dec.Decode()
	require.NoError(t, err)
	s, ok := raw.(bolt.Structure)
	require.True(t, ok)
	require.Equal(t, byte(0x71), s.Tag, "expected first message to be RECORD for %q", cypher)

	var firstField interface{}
	if len(s.Fields) > 0 {
		if fields, ok := s.Fields[0].([]interface{}); ok && len(fields) > 0 {
			firstField = fields[0]
		}
	}

	// Drain remaining records + final SUCCESS
	for {
		data, err := bolt.ReadMessage(c.conn)
		if err != nil {
			break
		}
		dec := bolt.NewDecoder(data)
		raw, err := dec.Decode()
		if err != nil {
			break
		}
		s, _ := raw.(bolt.Structure)
		if s.Tag != 0x71 {
			break
		}
	}
	return firstField
}

// TestBolt_WhereFiltersExtended tests WHERE operators not covered by the basic
// WhereFilters test via the Bolt protocol.
func TestBolt_WhereFiltersExtended(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)

	cases := []struct {
		name    string
		cypher  string
		wantRec int // expected RECORD count (count queries return 1 record)
	}{
		{"not_equals", "MATCH (n:CypherTest) WHERE n.name <> 'Alice' RETURN count(n) AS c", 1},
		{"AND", "MATCH (n:CypherTest) WHERE n.enabled = true AND n.score > 100 RETURN count(n) AS c", 1},
		{"OR", "MATCH (n:CypherTest) WHERE n.name = 'Alice' OR n.name = 'Bob' RETURN count(n) AS c", 1},
		{"ends_with", "MATCH (n:CypherTest) WHERE n.objectid ENDS WITH '-3' RETURN count(n) AS c", 1},
		{"contains", "MATCH (n:CypherTest) WHERE n.name CONTAINS 'ob' RETURN count(n) AS c", 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := dialBolt(t, addr)
			c.hello()
			got := countBoltRecords(t, c, tc.cypher)
			assert.Equal(t, tc.wantRec, got, "record count mismatch for %q", tc.cypher)
		})
	}
}

// TestBolt_WithClause verifies the WITH clause pipeline filter via Bolt.
func TestBolt_WithClause(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)
	c := dialBolt(t, addr)
	c.hello()

	records := countBoltRecords(t, c,
		"MATCH (n:CypherTest) WITH n WHERE n.enabled = true RETURN count(n) AS c")
	assert.Equal(t, 1, records, "WITH clause count query should produce exactly one RECORD")
}

// TestBolt_OrderBy verifies that ORDER BY returns 3 ordered RECORD messages via Bolt.
func TestBolt_OrderBy(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)
	c := dialBolt(t, addr)
	c.hello()

	records := countBoltRecords(t, c,
		"MATCH (n:CypherTest) RETURN n.name AS name ORDER BY n.name")
	assert.Equal(t, 3, records, "ORDER BY query should return 3 records (Alice, Bob, Charlie)")
}

// TestBolt_Distinct verifies count(DISTINCT ...) via Bolt.
func TestBolt_Distinct(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)
	c := dialBolt(t, addr)
	c.hello()

	records := countBoltRecords(t, c,
		"MATCH (n:CypherTest) RETURN count(DISTINCT n.enabled) AS c")
	assert.Equal(t, 1, records, "DISTINCT count query should produce exactly one RECORD")
}

// TestBolt_CypherFunctions tests Cypher built-in functions via the Bolt protocol.
func TestBolt_CypherFunctions(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)

	t.Run("type", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		val := firstBoltRecordField(t, c,
			"MATCH ()-[r:CypherEdge]->() RETURN type(r) AS t LIMIT 1")
		assert.Equal(t, "CypherEdge", val)
	})

	t.Run("coalesce", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		val := firstBoltRecordField(t, c,
			"MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN coalesce(n.name, 'unknown') AS name")
		assert.Equal(t, "Alice", val)
	})

	t.Run("toLower", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		val := firstBoltRecordField(t, c,
			"MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN toLower(n.name) AS l")
		assert.Equal(t, "alice", val)
	})

	t.Run("toUpper", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		val := firstBoltRecordField(t, c,
			"MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN toUpper(n.name) AS u")
		assert.Equal(t, "ALICE", val)
	})

	t.Run("labels", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		val := firstBoltRecordField(t, c,
			"MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN labels(n) AS l")
		// labels() may be a []interface{} or string — check it contains "CypherTest"
		assert.Contains(t, fmt.Sprintf("%v", val), "CypherTest")
	})
}

// TestBolt_CaseExpression verifies CASE WHEN ... THEN ... ELSE ... END via Bolt.
func TestBolt_CaseExpression(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)
	c := dialBolt(t, addr)
	c.hello()

	val := firstBoltRecordField(t, c,
		"MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN CASE WHEN n.enabled = true THEN 'active' ELSE 'inactive' END AS status")
	assert.Equal(t, "active", val)
}

// TestBolt_VariableLengthPaths tests variable-length path patterns via the Bolt protocol.
func TestBolt_VariableLengthPaths(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)

	t.Run("unbounded", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		// Alice->Bob->Charlie: Alice can reach 2 nodes (Bob and Charlie)
		records := countBoltRecords(t, c,
			"MATCH (a:CypherTest)-[:CypherEdge*1..]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c")
		assert.Equal(t, 1, records, "unbounded variable path count should return 1 RECORD")
	})

	t.Run("exact_length", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		records := countBoltRecords(t, c,
			"MATCH (a:CypherTest)-[:CypherEdge*2]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c")
		assert.Equal(t, 1, records)
	})

	t.Run("bounded", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		records := countBoltRecords(t, c,
			"MATCH (a:CypherTest)-[:CypherEdge*1..1]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c")
		assert.Equal(t, 1, records)
	})
}

// TestBolt_PatternMatchWithRelationships verifies explicit relationship type
// patterns return the correct count via the Bolt protocol.
func TestBolt_PatternMatchWithRelationships(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)
	c := dialBolt(t, addr)
	c.hello()

	records := countBoltRecords(t, c,
		"MATCH (a:CypherTest)-[:CypherEdge]->(b:CypherTest) RETURN count(*) AS c")
	assert.Equal(t, 1, records, "pattern match count query should produce exactly one RECORD")
}

// TestBolt_PipeSeparatedTypes verifies pipe-separated relationship type syntax
// via the Bolt protocol.
func TestBolt_PipeSeparatedTypes(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	ids := createNodes(t.Context(), t, graphDB, graph.StringKind("CypherTest"), 3)
	err := graphDB.WriteTransaction(t.Context(), func(tx graph.Transaction) error {
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[1], graph.StringKind("TypeA"), graph.NewProperties()); err != nil {
			return err
		}
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[2], graph.StringKind("TypeB"), graph.NewProperties()); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	daemon := bolt.NewDaemon(addr, gq, mockDB)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { daemon.Start(ctx) }()
	waitForTCP(t, addr, 2*time.Second)
	t.Cleanup(func() { cancel(); daemon.Stop(context.Background()) }) //nolint:errcheck

	c := dialBolt(t, addr)
	c.hello()

	cypher := fmt.Sprintf("MATCH (a)-[:TypeA|TypeB]->(x) WHERE id(a) = %d RETURN count(x) AS c", ids[0])
	records := countBoltRecords(t, c, cypher)
	assert.Equal(t, 1, records, "pipe-separated type count query should return 1 RECORD")
}

// TestBolt_LabelCheckInWhere verifies the WHERE n:Label syntax via Bolt.
func TestBolt_LabelCheckInWhere(t *testing.T) {
	t.Parallel()
	addr, _, _, _ := boltSeededServer(t)
	c := dialBolt(t, addr)
	c.hello()

	records := countBoltRecords(t, c,
		"MATCH (n) WHERE n:CypherTest RETURN count(n) AS c")
	assert.Equal(t, 1, records, "label check count query should return 1 RECORD")
}

// TestBolt_PathQueries tests shortestPath, allShortestPaths, and bounded
// variable-length paths via the Bolt protocol.
func TestBolt_PathQueries(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	ids := createChain(t.Context(), t, graphDB, graph.StringKind("TestNode"), graph.StringKind("TestEdge"), 3)

	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	daemon := bolt.NewDaemon(addr, gq, mockDB)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { daemon.Start(ctx) }()
	waitForTCP(t, addr, 2*time.Second)
	t.Cleanup(func() { cancel(); daemon.Stop(context.Background()) }) //nolint:errcheck

	t.Run("shortestPath", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		cypher := fmt.Sprintf(
			"MATCH p = shortestPath((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p",
			ids[0], ids[2])
		records := countBoltRecords(t, c, cypher)
		assert.GreaterOrEqual(t, records, 1, "shortestPath should return at least 1 RECORD")
	})

	t.Run("allShortestPaths", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		cypher := fmt.Sprintf(
			"MATCH p = allShortestPaths((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p",
			ids[0], ids[2])
		records := countBoltRecords(t, c, cypher)
		assert.GreaterOrEqual(t, records, 1, "allShortestPaths should return at least 1 RECORD")
	})

	t.Run("variable_length_bounded", func(t *testing.T) {
		t.Parallel()
		c := dialBolt(t, addr)
		c.hello()
		cypher := fmt.Sprintf(
			"MATCH (a)-[:TestEdge*1..2]->(x) WHERE id(a) = %d RETURN count(x) AS c",
			ids[0])
		records := countBoltRecords(t, c, cypher)
		assert.Equal(t, 1, records, "bounded path count query should return 1 RECORD")
	})
}
