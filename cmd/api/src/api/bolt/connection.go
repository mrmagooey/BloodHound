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
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
)

// Bolt protocol handshake magic bytes
var boltMagic = []byte{0x60, 0x60, 0xB0, 0x17}

// Supported Bolt version: 4.4
const (
	boltVersion44 uint32 = 0x00000404 // minor=4, major=4
)

// Connection states
type connState int

const (
	stateNegotiation connState = iota
	stateReady
	stateStreaming
	stateTxReady
	stateTxStreaming
	stateFailed
	stateDefunct
)

// bufferedResult holds the results of a RUN command waiting for PULL/DISCARD.
type bufferedResult struct {
	columns []string
	rows    [][]interface{}
}

// Connection manages a single Bolt TCP connection.
type Connection struct {
	conn       net.Conn
	graphQuery queries.Graph
	db         database.Database
	state      connState
	ctx        context.Context

	// Buffered query results waiting for PULL
	pendingResult *bufferedResult
}

// NewConnection creates a new Bolt connection handler.
func NewConnection(ctx context.Context, conn net.Conn, graphQuery queries.Graph, db database.Database) *Connection {
	return &Connection{
		conn:       conn,
		graphQuery: graphQuery,
		db:         db,
		state:      stateNegotiation,
		ctx:        ctx,
	}
}

// Handle runs the connection lifecycle: handshake, then message loop.
func (c *Connection) Handle() {
	defer c.conn.Close()

	if err := c.handshake(); err != nil {
		slog.Error("Bolt handshake failed", slog.String("remote", c.conn.RemoteAddr().String()), slog.String("error", err.Error()))
		return
	}

	c.messageLoop()
}

// handshake performs the Bolt protocol version negotiation.
func (c *Connection) handshake() error {
	// Read 4-byte magic preamble
	magic := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, magic); err != nil {
		return fmt.Errorf("failed to read magic bytes: %w", err)
	}

	if magic[0] != boltMagic[0] || magic[1] != boltMagic[1] || magic[2] != boltMagic[2] || magic[3] != boltMagic[3] {
		return fmt.Errorf("invalid magic bytes: %x", magic)
	}

	// Read 4 x 32-bit version proposals
	versions := make([]byte, 16)
	if _, err := io.ReadFull(c.conn, versions); err != nil {
		return fmt.Errorf("failed to read version proposals: %w", err)
	}

	// Check if any proposed version matches 4.4
	selectedVersion := uint32(0)
	for i := 0; i < 4; i++ {
		proposed := binary.BigEndian.Uint32(versions[i*4 : (i+1)*4])

		// Version encoding: major in bits 0-7, minor in bits 8-15, range in bits 16-23
		major := proposed & 0xFF
		minor := (proposed >> 8) & 0xFF
		rangeVal := (proposed >> 16) & 0xFF

		if major == 4 {
			// Check if 4.4 falls within the range [major.(minor-range) .. major.minor]
			minMinor := minor - rangeVal
			if minor >= 4 && minMinor <= 4 {
				selectedVersion = boltVersion44
				break
			}
		}
	}

	// Send selected version (or 0 to reject)
	var resp [4]byte
	binary.BigEndian.PutUint32(resp[:], selectedVersion)
	if _, err := c.conn.Write(resp[:]); err != nil {
		return fmt.Errorf("failed to write version response: %w", err)
	}

	if selectedVersion == 0 {
		return fmt.Errorf("no compatible Bolt version found")
	}

	slog.Debug("Bolt handshake complete", slog.String("remote", c.conn.RemoteAddr().String()), slog.String("version", "4.4"))
	return nil
}

// messageLoop reads and dispatches messages until the connection closes.
func (c *Connection) messageLoop() {
	for c.state != stateDefunct {
		data, err := ReadMessage(c.conn)
		if err != nil {
			if err != io.EOF && !isConnClosed(err) {
				slog.Error("Bolt read error", slog.String("remote", c.conn.RemoteAddr().String()), slog.String("error", err.Error()))
			}
			return
		}

		tag, msg, err := DecodeClientMessage(data)
		if err != nil {
			slog.Error("Bolt message decode error", slog.String("error", err.Error()))
			c.sendFailure("Neo.ClientError.Request.Invalid", err.Error())
			c.state = stateFailed
			continue
		}

		if err := c.dispatch(tag, msg); err != nil {
			slog.Error("Bolt dispatch error", slog.String("error", err.Error()))
		}
	}
}

func (c *Connection) dispatch(tag byte, msg interface{}) error {
	// GOODBYE is always handled
	if tag == msgGoodbye {
		c.state = stateDefunct
		return nil
	}

	// RESET always returns to READY
	if tag == msgReset {
		c.pendingResult = nil
		c.state = stateReady
		return c.sendSuccess(map[string]interface{}{})
	}

	// In FAILED state, ignore everything except RESET and GOODBYE
	if c.state == stateFailed {
		return c.sendIgnored()
	}

	switch tag {
	case msgHello:
		return c.handleHello(msg.(HelloMessage))
	case msgRun:
		return c.handleRun(msg.(RunMessage))
	case msgPull:
		return c.handlePull(msg.(PullMessage))
	case msgDiscard:
		return c.handleDiscard(msg.(DiscardMessage))
	case msgBegin:
		return c.handleBegin(msg.(BeginMessage))
	case msgCommit:
		return c.handleCommit()
	case msgRollback:
		return c.handleRollback()
	default:
		return c.sendFailure("Neo.ClientError.Request.Invalid", fmt.Sprintf("unexpected message 0x%02X in state %d", tag, c.state))
	}
}

func (c *Connection) handleHello(msg HelloMessage) error {
	if c.state != stateNegotiation {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "HELLO not expected in current state")
	}

	c.state = stateReady

	metadata := map[string]interface{}{
		"server":        "BloodHound/5.0.0",
		"connection_id": c.conn.RemoteAddr().String(),
	}

	return c.sendSuccess(metadata)
}

func (c *Connection) handleRun(msg RunMessage) error {
	if c.state != stateReady && c.state != stateTxReady {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "RUN not expected in current state")
	}

	// Execute the cypher query
	columns, rows, err := c.executeCypher(msg.Query, msg.Parameters)
	if err != nil {
		c.state = stateFailed
		return c.sendFailure("Neo.DatabaseError.Statement.ExecutionFailed", err.Error())
	}

	// Buffer results for PULL
	c.pendingResult = &bufferedResult{
		columns: columns,
		rows:    rows,
	}

	if c.state == stateReady {
		c.state = stateStreaming
	} else {
		c.state = stateTxStreaming
	}

	metadata := map[string]interface{}{
		"fields":  toInterfaceSlice(columns),
		"t_first": int64(0),
	}
	return c.sendSuccess(metadata)
}

func (c *Connection) handlePull(msg PullMessage) error {
	if c.state != stateStreaming && c.state != stateTxStreaming {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "PULL not expected in current state")
	}

	n := int64(-1) // default: pull all
	if msg.Extra != nil {
		if nVal, ok := msg.Extra["n"]; ok {
			if nInt, ok := nVal.(int64); ok {
				n = nInt
			}
		}
	}

	result := c.pendingResult
	if result == nil {
		if c.state == stateStreaming {
			c.state = stateReady
		} else {
			c.state = stateTxReady
		}
		return c.sendSuccess(map[string]interface{}{})
	}

	// Stream records
	count := int64(0)
	hasMore := false
	for i, row := range result.rows {
		if n >= 0 && count >= n {
			// Truncate: keep remaining rows
			result.rows = result.rows[i:]
			hasMore = true
			break
		}
		if err := c.sendRecord(row); err != nil {
			return err
		}
		count++
	}

	if !hasMore {
		c.pendingResult = nil
		if c.state == stateStreaming {
			c.state = stateReady
		} else {
			c.state = stateTxReady
		}
	}

	metadata := map[string]interface{}{
		"has_more": hasMore,
	}
	return c.sendSuccess(metadata)
}

func (c *Connection) handleDiscard(msg DiscardMessage) error {
	if c.state != stateStreaming && c.state != stateTxStreaming {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "DISCARD not expected in current state")
	}

	c.pendingResult = nil
	if c.state == stateStreaming {
		c.state = stateReady
	} else {
		c.state = stateTxReady
	}

	return c.sendSuccess(map[string]interface{}{
		"has_more": false,
	})
}

func (c *Connection) handleBegin(msg BeginMessage) error {
	if c.state != stateReady {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "BEGIN not expected in current state")
	}
	c.state = stateTxReady
	return c.sendSuccess(map[string]interface{}{})
}

func (c *Connection) handleCommit() error {
	if c.state != stateTxReady {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "COMMIT not expected in current state")
	}
	c.state = stateReady
	return c.sendSuccess(map[string]interface{}{
		"bookmark": "bh:latest",
	})
}

func (c *Connection) handleRollback() error {
	if c.state != stateTxReady {
		return c.sendFailure("Neo.ClientError.Request.Invalid", "ROLLBACK not expected in current state")
	}
	c.state = stateReady
	return c.sendSuccess(map[string]interface{}{})
}

// executeCypher runs a Cypher query through the BloodHound graph query layer.
// Returns column names and rows of values.
func (c *Connection) executeCypher(query string, parameters map[string]interface{}) ([]string, [][]interface{}, error) {
	validPrimaryKinds, err := c.db.GetDisplayNodeGraphKinds(c.ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get display node kinds: %w", err)
	}

	preparedQuery, err := c.graphQuery.PrepareCypherQuery(query, queries.DefaultQueryFitnessLowerBoundExplore)
	if err != nil {
		return nil, nil, err
	}

	graphResponse, err := c.graphQuery.RawCypherQuery(c.ctx, validPrimaryKinds, preparedQuery, true)
	if err != nil {
		return nil, nil, err
	}

	return convertGraphResponseToRecords(graphResponse)
}

// convertGraphResponseToRecords converts a UnifiedGraph into column names and row data
// suitable for Bolt RECORD messages. Nodes are returned as PackStream Node structures,
// relationships as Relationship structures.
func convertGraphResponseToRecords(graphResponse model.UnifiedGraph) ([]string, [][]interface{}, error) {
	// Handle literal results
	if len(graphResponse.Literals) > 0 {
		return convertLiteralsToRecords(graphResponse)
	}

	// Handle nodes only
	if len(graphResponse.Nodes) > 0 && len(graphResponse.Edges) == 0 {
		return convertNodesToRecords(graphResponse)
	}

	// Handle edges with source/target
	if len(graphResponse.Edges) > 0 {
		return convertEdgesToRecords(graphResponse)
	}

	return []string{}, [][]interface{}{}, nil
}

func convertLiteralsToRecords(graphResponse model.UnifiedGraph) ([]string, [][]interface{}, error) {
	// Determine column names by scanning until we see a repeated key,
	// which signals the start of a second row.
	columns := []string{}
	columnIndex := map[string]int{}

	for _, lit := range graphResponse.Literals {
		if _, exists := columnIndex[lit.Key]; exists {
			break
		}
		columnIndex[lit.Key] = len(columns)
		columns = append(columns, lit.Key)
	}

	if len(columns) == 0 {
		return columns, [][]interface{}{}, nil
	}

	// Group literals into rows using the column count
	numCols := len(columns)
	numLits := len(graphResponse.Literals)
	rows := make([][]interface{}, 0, (numLits+numCols-1)/numCols)

	for i := 0; i < numLits; i += numCols {
		row := make([]interface{}, numCols)
		for j := 0; j < numCols && i+j < numLits; j++ {
			lit := graphResponse.Literals[i+j]
			colIdx := columnIndex[lit.Key]
			row[colIdx] = lit.Value
		}
		rows = append(rows, row)
	}

	return columns, rows, nil
}

func convertNodesToRecords(graphResponse model.UnifiedGraph) ([]string, [][]interface{}, error) {
	columns := []string{"n"}
	var rows [][]interface{}

	for idStr, node := range graphResponse.Nodes {
		nodeID := parseNodeID(idStr)
		labels := node.Kinds
		if len(labels) == 0 && node.Kind != "" {
			labels = []string{node.Kind}
		}

		props := buildNodeProperties(node)

		boltNode := Structure{
			Tag: tagNode,
			Fields: []interface{}{
				nodeID,                      // id
				toInterfaceSlice(labels),    // labels
				props,                       // properties
			},
		}
		rows = append(rows, []interface{}{boltNode})
	}

	return columns, rows, nil
}

func convertEdgesToRecords(graphResponse model.UnifiedGraph) ([]string, [][]interface{}, error) {
	columns := []string{"r", "source", "target"}
	var rows [][]interface{}

	for i, edge := range graphResponse.Edges {
		// Build relationship
		sourceID := parseNodeID(edge.Source)
		targetID := parseNodeID(edge.Target)
		relProps := map[string]interface{}{}
		if edge.Properties != nil {
			for k, v := range edge.Properties {
				relProps[k] = v
			}
		}

		boltRel := Structure{
			Tag: tagRelationship,
			Fields: []interface{}{
				int64(i),    // id
				sourceID,    // startNodeId
				targetID,    // endNodeId
				edge.Kind,   // type
				relProps,    // properties
			},
		}

		// Build source node
		var sourceNode interface{}
		if sn, ok := graphResponse.Nodes[edge.Source]; ok {
			labels := sn.Kinds
			if len(labels) == 0 && sn.Kind != "" {
				labels = []string{sn.Kind}
			}
			sourceNode = Structure{
				Tag: tagNode,
				Fields: []interface{}{
					sourceID,
					toInterfaceSlice(labels),
					buildNodeProperties(sn),
				},
			}
		}

		// Build target node
		var targetNode interface{}
		if tn, ok := graphResponse.Nodes[edge.Target]; ok {
			labels := tn.Kinds
			if len(labels) == 0 && tn.Kind != "" {
				labels = []string{tn.Kind}
			}
			targetNode = Structure{
				Tag: tagNode,
				Fields: []interface{}{
					targetID,
					toInterfaceSlice(labels),
					buildNodeProperties(tn),
				},
			}
		}

		rows = append(rows, []interface{}{boltRel, sourceNode, targetNode})
	}

	return columns, rows, nil
}

func buildNodeProperties(node model.UnifiedNode) map[string]interface{} {
	props := map[string]interface{}{}
	if node.Properties != nil {
		for k, v := range node.Properties {
			props[k] = v
		}
	}
	props["name"] = node.Label
	props["objectid"] = node.ObjectId
	if node.Kind != "" {
		props["_kind"] = node.Kind
	}
	if len(node.Kinds) > 0 {
		props["_labels"] = strings.Join(node.Kinds, ":")
	}
	return props
}

func parseNodeID(idStr string) int64 {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

// sendSuccess sends a SUCCESS message to the client.
func (c *Connection) sendSuccess(metadata map[string]interface{}) error {
	data, err := EncodeSuccess(metadata)
	if err != nil {
		return err
	}
	return WriteMessage(c.conn, data)
}

// sendFailure sends a FAILURE message to the client.
func (c *Connection) sendFailure(code, message string) error {
	data, err := EncodeFailure(code, message)
	if err != nil {
		return err
	}
	return WriteMessage(c.conn, data)
}

// sendIgnored sends an IGNORED message to the client.
func (c *Connection) sendIgnored() error {
	data, err := EncodeIgnored()
	if err != nil {
		return err
	}
	return WriteMessage(c.conn, data)
}

// sendRecord sends a RECORD message to the client.
func (c *Connection) sendRecord(fields []interface{}) error {
	data, err := EncodeRecord(fields)
	if err != nil {
		return err
	}
	return WriteMessage(c.conn, data)
}

// isConnClosed checks if an error indicates the connection was closed.
func isConnClosed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "connection reset by peer")
}
