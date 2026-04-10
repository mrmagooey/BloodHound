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

//go:build standalone

package ad_test

import (
	"context"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/analysis/ad"
	"github.com/specterops/bloodhound/packages/go/analysis"
	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/stretchr/testify/require"
)

// TestPostParallelEmptyGraph verifies that the parallel Post() pipeline completes
// without errors or deadlocks on an empty graph. This exercises the errgroup
// plumbing that runs Phase 2 steps concurrently.
func TestPostParallelEmptyGraph(t *testing.T) {
	ctx := context.Background()

	// Open an in-memory kglite database (empty string = in-memory).
	db, err := kglitedawgs.Open("")
	require.NoError(t, err)
	defer db.Close(ctx)

	counter := analysis.NewCompositionCounter()

	// Run Post on an empty graph — all steps should complete without error.
	stats, err := ad.Post(ctx, db, true /* adcsEnabled */, false /* citrixEnabled */, true /* ntlmEnabled */, &counter)
	require.NoError(t, err)
	require.NotNil(t, stats, "Post() must return non-nil stats")
}
