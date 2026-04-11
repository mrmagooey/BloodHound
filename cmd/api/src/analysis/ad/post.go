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

package ad

import (
	"context"
	"log/slog"
	"sync"

	"github.com/specterops/bloodhound/packages/go/analysis"
	adAnalysis "github.com/specterops/bloodhound/packages/go/analysis/ad"
	"github.com/specterops/bloodhound/packages/go/bhlog/attr"
	"github.com/specterops/bloodhound/packages/go/bhlog/measure"
	"github.com/specterops/bloodhound/packages/go/graphschema/ad"
	"github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/dawgs/graph"
	"golang.org/x/sync/errgroup"
)

func Post(ctx context.Context, db graph.Database, adcsEnabled, citrixEnabled, ntlmEnabled bool, compositionCounter *analysis.CompositionCounter) (*analysis.AtomicPostProcessingStats, error) {
	defer measure.ContextLogAndMeasure(
		ctx,
		slog.LevelDebug,
		"Active Directory Post Processing",
		attr.Namespace("analysis"),
		attr.Function("Post"),
		attr.Scope("step"),
	)()

	aggregateStats := analysis.NewAtomicPostProcessingStats()

	// Phase 1: sequential prerequisites
	if err := adAnalysis.FixWellKnownNodeTypes(ctx, db); err != nil {
		return &aggregateStats, err
	} else if err := adAnalysis.RunDomainAssociations(ctx, db); err != nil {
		return &aggregateStats, err
	} else if err := adAnalysis.LinkWellKnownNodes(ctx, db); err != nil {
		return &aggregateStats, err
	} else if deleteTransitEdgesStats, err := analysis.DeleteTransitEdges(ctx, db, graph.Kinds{ad.Entity, azure.Entity}, ad.PostProcessedRelationships()); err != nil {
		return &aggregateStats, err
	} else if localGroupData, err := adAnalysis.FetchLocalGroupData(ctx, db); err != nil {
		return &aggregateStats, err
	} else if domainNodes, err := adAnalysis.FetchCollectedDomainNodes(ctx, db); err != nil {
		return &aggregateStats, err
	} else {
		aggregateStats.Merge(deleteTransitEdgesStats)

		// Phase 2: parallel independent steps + PostADCS (which returns adcsCache needed by PostNTLM)
		var (
			mu       sync.Mutex
			adcsCache adAnalysis.ADCSCache
		)

		eg, egCtx := errgroup.WithContext(ctx)

		eg.Go(func() error {
			stats, err := adAnalysis.PostDCSync(egCtx, db, localGroupData, domainNodes)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, err := adAnalysis.PostProtectAdminGroups(egCtx, db, domainNodes)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, err := adAnalysis.PostSyncLAPSPassword(egCtx, db, localGroupData, domainNodes)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, err := adAnalysis.PostHasTrustKeys(egCtx, db, domainNodes)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, err := adAnalysis.PostLocalGroups(egCtx, db, localGroupData)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, err := adAnalysis.PostCanRDP(egCtx, db, localGroupData, true, citrixEnabled)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, err := adAnalysis.PostOwnsAndWriteOwner(egCtx, db, localGroupData)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			mu.Unlock()
			return nil
		})

		eg.Go(func() error {
			stats, cache, err := adAnalysis.PostADCS(egCtx, db, localGroupData, adcsEnabled)
			if err != nil {
				return err
			}
			mu.Lock()
			aggregateStats.Merge(stats)
			adcsCache = cache
			mu.Unlock()
			return nil
		})

		if err := eg.Wait(); err != nil {
			return &aggregateStats, err
		}

		// Phase 3: PostNTLM depends on adcsCache from PostADCS
		if ntlmStats, err := adAnalysis.PostNTLM(ctx, db, localGroupData, adcsCache, ntlmEnabled, compositionCounter); err != nil {
			return &aggregateStats, err
		} else {
			aggregateStats.Merge(ntlmStats)
		}

		return &aggregateStats, nil
	}
}
