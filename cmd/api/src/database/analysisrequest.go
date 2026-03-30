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

package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"gorm.io/gorm"
)

type AnalysisRequestData interface {
	DeleteAnalysisRequest(ctx context.Context) error
	GetAnalysisRequest(ctx context.Context) (model.AnalysisRequest, error)
	HasAnalysisRequest(ctx context.Context) bool
	HasCollectedGraphDataDeletionRequest(ctx context.Context) (model.AnalysisRequest, bool)
	RequestAnalysis(ctx context.Context, requester string) error
	RequestCollectedGraphDataDeletion(ctx context.Context, request model.AnalysisRequest) error
}

func (s *BloodhoundDB) DeleteAnalysisRequest(ctx context.Context) error {
	db := s.db.WithContext(ctx)
	if s.isSQLite() {
		return db.Exec(`DELETE FROM analysis_request_switch;`).Error
	}
	return db.Exec(`TRUNCATE analysis_request_switch;`).Error
}

func (s *BloodhoundDB) GetAnalysisRequest(ctx context.Context) (model.AnalysisRequest, error) {
	var analysisRequest model.AnalysisRequest

	tx := s.db.WithContext(ctx).Select("requested_by, request_type, requested_at").Table("analysis_request_switch").First(&analysisRequest)

	return analysisRequest, CheckError(tx)
}

func (s *BloodhoundDB) HasAnalysisRequest(ctx context.Context) bool {
	var exists bool

	tx := s.db.WithContext(ctx).Raw(`select exists(select * from analysis_request_switch where request_type = ? limit 1);`, model.AnalysisRequestAnalysis).Scan(&exists)
	if tx.Error != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("Error determining if there's an analysis request: %v", tx.Error))
	}
	return exists
}

func (s *BloodhoundDB) HasCollectedGraphDataDeletionRequest(ctx context.Context) (model.AnalysisRequest, bool) {
	var record model.AnalysisRequest

	tx := s.db.WithContext(ctx).Raw(`select * from analysis_request_switch where request_type = ? limit 1;`, model.AnalysisRequestDeletion).First(&record)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return record, false
		}
		slog.ErrorContext(ctx, fmt.Sprintf("Error querying deletion request: %v", tx.Error))
		return record, false
	}
	return record, true
}

// setAnalysisRequest inserts a row into analysis_request_switch for both a collected graph data deletion request or an analysis request.
// There should only ever be 1 row, if a request is present, subsequent requests no-op
// If an analysis request is present when a deletion request comes in, that overwrites the analysis to deletion but not vice-versa
// To request: Use the helper methods `RequestAnalysis` and `RequestCollectedGraphDataDeletion`
func (s *BloodhoundDB) setAnalysisRequest(ctx context.Context, request model.AnalysisRequest) error {
	db := s.db.WithContext(ctx)
	now := time.Now().UTC()

	if s.isSQLite() {
		return s.setAnalysisRequestSQLite(ctx, db, request, now)
	}

	args := []any{
		request.RequestedBy,
		request.RequestType,
		now,
		request.DeleteAllGraph,
		request.DeleteSourcelessGraph,
		pq.StringArray([]string(request.DeleteSourceKinds)),
	}

	insertSQL := `
	INSERT INTO analysis_request_switch (
		requested_by,
		request_type,
		requested_at,
		delete_all_graph,
		delete_sourceless_graph,
		delete_source_kinds
	)
	VALUES (?, ?, ?, ?, ?, ?::text[]);`
	updateSQL := `UPDATE analysis_request_switch
	SET
		requested_by = ?,
		request_type = ?,
		requested_at = ?,
		delete_all_graph = ?,
		delete_sourceless_graph = ?,
		delete_source_kinds = ?::text[];`

	if analysisRequest, err := s.GetAnalysisRequest(ctx); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	} else if errors.Is(err, ErrNotFound) {
		return db.Exec(insertSQL, args...).Error
	} else {
		if analysisRequest.RequestType == model.AnalysisRequestAnalysis && request.RequestType == model.AnalysisRequestDeletion {
			return db.Exec(updateSQL, args...).Error
		}
		return nil
	}
}

// setAnalysisRequestSQLite uses SQLite-compatible UPSERT (no PostgreSQL type casts).
func (s *BloodhoundDB) setAnalysisRequestSQLite(ctx context.Context, db *gorm.DB, request model.AnalysisRequest, now time.Time) error {
	kindsJSON, err := json.Marshal(request.DeleteSourceKinds)
	if err != nil {
		return err
	}

	if analysisRequest, err := s.GetAnalysisRequest(ctx); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	} else if errors.Is(err, ErrNotFound) {
		return db.Exec(`
			INSERT INTO analysis_request_switch
				(singleton, requested_by, request_type, requested_at, delete_all_graph, delete_sourceless_graph, delete_source_kinds)
			VALUES (1, ?, ?, ?, ?, ?, ?)`,
			request.RequestedBy, request.RequestType, now,
			request.DeleteAllGraph, request.DeleteSourcelessGraph, string(kindsJSON),
		).Error
	} else if analysisRequest.RequestType == model.AnalysisRequestAnalysis && request.RequestType == model.AnalysisRequestDeletion {
		return db.Exec(`
			UPDATE analysis_request_switch
			SET requested_by=?, request_type=?, requested_at=?, delete_all_graph=?, delete_sourceless_graph=?, delete_source_kinds=?`,
			request.RequestedBy, request.RequestType, now,
			request.DeleteAllGraph, request.DeleteSourcelessGraph, string(kindsJSON),
		).Error
	}
	return nil
}

// RequestAnalysis will request an analysis be executed, as long as there isn't an existing analysis request or collected graph data deletion request, then it no-ops
func (s *BloodhoundDB) RequestAnalysis(ctx context.Context, requestedBy string) error {
	slog.InfoContext(ctx, fmt.Sprintf("Analysis requested by %s", requestedBy))
	return s.setAnalysisRequest(ctx, model.AnalysisRequest{RequestType: model.AnalysisRequestAnalysis, RequestedBy: requestedBy})
}

// RequestCollectedGraphDataDeletion will request collected graph data be deleted, if an analysis request is present, it will overwrite that.
func (s *BloodhoundDB) RequestCollectedGraphDataDeletion(ctx context.Context, request model.AnalysisRequest) error {
	slog.InfoContext(ctx, fmt.Sprintf("Collected graph data deletion requested by %s", request.RequestedBy))
	return s.setAnalysisRequest(ctx, request)
}
