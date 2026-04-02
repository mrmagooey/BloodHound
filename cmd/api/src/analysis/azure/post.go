// Copyright 2023 Specter Ops, Inc.
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

package azure

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/specterops/bloodhound/packages/go/analysis"
	azureAnalysis "github.com/specterops/bloodhound/packages/go/analysis/azure"
	"github.com/specterops/bloodhound/packages/go/analysis/hybrid"
	"github.com/specterops/bloodhound/packages/go/bhlog/attr"
	"github.com/specterops/bloodhound/packages/go/bhlog/measure"
	"github.com/specterops/bloodhound/packages/go/graphschema/ad"
	"github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/dawgs/graph"
)

func Post(ctx context.Context, db graph.Database) (*analysis.AtomicPostProcessingStats, error) {
	defer measure.ContextLogAndMeasure(
		ctx,
		slog.LevelInfo,
		"Azure Post Processing",
		attr.Namespace("analysis"),
		attr.Function("Post"),
		attr.Scope("step"),
	)()

	aggregateStats := analysis.NewAtomicPostProcessingStats()

	timeStage := func(name string, fn func() error) error {
		start := time.Now()
		err := fn()
		fmt.Printf("AZURE-STAGE: %-45s %v\n", name, time.Since(start))
		return err
	}

	if err := timeStage("FixManagementGroupNames", func() error {
		return azureAnalysis.FixManagementGroupNames(ctx, db)
	}); err != nil {
		slog.WarnContext(ctx, "Error fixing management group names", attr.Error(err))
	}

	var stats, userRoleStats, executeCommandStats, appRoleAssignmentStats, hybridStats, pimRolesStats *analysis.AtomicPostProcessingStats
	var err error

	if err = timeStage("DeleteTransitEdges", func() error {
		stats, err = analysis.DeleteTransitEdges(ctx, db, graph.Kinds{ad.Entity, azure.Entity}, azure.PostProcessedRelationships())
		return err
	}); err != nil {
		return &aggregateStats, err
	}

	if err = timeStage("UserRoleAssignments", func() error {
		userRoleStats, err = azureAnalysis.UserRoleAssignments(ctx, db)
		return err
	}); err != nil {
		return &aggregateStats, err
	}

	if err = timeStage("ExecuteCommand", func() error {
		executeCommandStats, err = azureAnalysis.ExecuteCommand(ctx, db)
		return err
	}); err != nil {
		return &aggregateStats, err
	}

	if err = timeStage("AppRoleAssignments", func() error {
		appRoleAssignmentStats, err = azureAnalysis.AppRoleAssignments(ctx, db)
		return err
	}); err != nil {
		return &aggregateStats, err
	}

	if err = timeStage("PostHybrid", func() error {
		hybridStats, err = hybrid.PostHybrid(ctx, db)
		return err
	}); err != nil {
		return &aggregateStats, err
	}

	if err = timeStage("CreateAZRoleApproverEdge", func() error {
		pimRolesStats, err = azureAnalysis.CreateAZRoleApproverEdge(ctx, db)
		return err
	}); err != nil {
		return &aggregateStats, err
	}

	aggregateStats.Merge(stats)
	aggregateStats.Merge(userRoleStats)
	aggregateStats.Merge(executeCommandStats)
	aggregateStats.Merge(appRoleAssignmentStats)
	aggregateStats.Merge(hybridStats)
	aggregateStats.Merge(pimRolesStats)
	return &aggregateStats, nil
}
