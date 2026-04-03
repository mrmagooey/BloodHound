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
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
)

const DefaultBoltPort = 7687

// Daemon implements the daemons.Daemon interface for the Bolt protocol server.
type Daemon struct {
	listenAddr string
	graphQuery queries.Graph
	db         database.Database

	listener net.Listener
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewDaemon creates a new Bolt protocol server daemon.
func NewDaemon(listenAddr string, graphQuery queries.Graph, db database.Database) *Daemon {
	return &Daemon{
		listenAddr: listenAddr,
		graphQuery: graphQuery,
		db:         db,
	}
}

// Name returns the name of this daemon.
func (d *Daemon) Name() string {
	return "Bolt Protocol Daemon"
}

// Start begins listening for Bolt connections.
func (d *Daemon) Start(ctx context.Context) {
	d.ctx, d.cancel = context.WithCancel(ctx)

	listener, err := net.Listen("tcp", d.listenAddr)
	if err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("Bolt server failed to listen on %s: %v", d.listenAddr, err))
		return
	}
	d.listener = listener

	slog.InfoContext(ctx, fmt.Sprintf("Bolt protocol server listening on %s", d.listenAddr))

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-d.ctx.Done():
				// Shutdown requested
				return
			default:
				slog.ErrorContext(ctx, fmt.Sprintf("Bolt server accept error: %v", err))
				continue
			}
		}

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			slog.DebugContext(ctx, fmt.Sprintf("Bolt connection accepted from %s", conn.RemoteAddr()))
			handler := NewConnection(d.ctx, conn, d.graphQuery, d.db)
			handler.Handle()
			slog.DebugContext(ctx, fmt.Sprintf("Bolt connection closed from %s", conn.RemoteAddr()))
		}()
	}
}

// Stop gracefully shuts down the Bolt server.
func (d *Daemon) Stop(ctx context.Context) error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.listener != nil {
		if err := d.listener.Close(); err != nil {
			return fmt.Errorf("bolt server listener close error: %w", err)
		}
	}
	d.wg.Wait()
	return nil
}
