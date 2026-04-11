// Copyright 2026 Specter Ops, Inc.
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

package datapipe

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSuccessStats returns a postFn that records a call and returns non-nil stats.
func newSuccessStats(called *atomic.Bool) postFn {
	s := analysis.NewAtomicPostProcessingStats()
	return func(_ context.Context) (*analysis.AtomicPostProcessingStats, error) {
		called.Store(true)
		return &s, nil
	}
}

// newErrorFn returns a postFn that records a call and returns an error.
func newErrorFn(called *atomic.Bool, err error) postFn {
	return func(_ context.Context) (*analysis.AtomicPostProcessingStats, error) {
		called.Store(true)
		return nil, err
	}
}

// TestRunPostProcessingConcurrently_BothCalled verifies that both post functions
// are invoked when running concurrently.
func TestRunPostProcessingConcurrently_BothCalled(t *testing.T) {
	t.Parallel()

	var (
		adCalled    atomic.Bool
		azureCalled atomic.Bool
	)

	adStats, azureStats, adErr, azureErr := runPostProcessingConcurrently(
		context.Background(),
		newSuccessStats(&adCalled),
		newSuccessStats(&azureCalled),
	)

	require.True(t, adCalled.Load(), "ad.Post should have been called")
	require.True(t, azureCalled.Load(), "azure.Post should have been called")
	require.NoError(t, adErr)
	require.NoError(t, azureErr)
	require.NotNil(t, adStats)
	require.NotNil(t, azureStats)
}

// TestRunPostProcessingConcurrently_ADErrorPropagates verifies that an error from
// the AD post function is returned in adErr while azureErr remains nil.
func TestRunPostProcessingConcurrently_ADErrorPropagates(t *testing.T) {
	t.Parallel()

	var (
		adCalled    atomic.Bool
		azureCalled atomic.Bool
		sentinel    = errors.New("ad post failure")
	)

	_, azureStats, adErr, azureErr := runPostProcessingConcurrently(
		context.Background(),
		newErrorFn(&adCalled, sentinel),
		newSuccessStats(&azureCalled),
	)

	require.True(t, adCalled.Load(), "ad.Post should have been called despite error")
	require.True(t, azureCalled.Load(), "azure.Post should still be called when ad fails")
	require.ErrorIs(t, adErr, sentinel)
	require.NoError(t, azureErr, "azure error should be nil when only AD fails")
	require.NotNil(t, azureStats)
}

// TestRunPostProcessingConcurrently_AzureErrorPropagates verifies that an error from
// the Azure post function is returned in azureErr while adErr remains nil.
func TestRunPostProcessingConcurrently_AzureErrorPropagates(t *testing.T) {
	t.Parallel()

	var (
		adCalled    atomic.Bool
		azureCalled atomic.Bool
		sentinel    = errors.New("azure post failure")
	)

	adStats, _, adErr, azureErr := runPostProcessingConcurrently(
		context.Background(),
		newSuccessStats(&adCalled),
		newErrorFn(&azureCalled, sentinel),
	)

	require.True(t, adCalled.Load(), "ad.Post should still be called when azure fails")
	require.True(t, azureCalled.Load(), "azure.Post should have been called despite error")
	require.NoError(t, adErr, "ad error should be nil when only Azure fails")
	require.ErrorIs(t, azureErr, sentinel)
	require.NotNil(t, adStats)
}

// TestRunPostProcessingConcurrently_BothErrorsIndependent verifies that errors from
// both post functions are captured independently — neither suppresses the other.
func TestRunPostProcessingConcurrently_BothErrorsIndependent(t *testing.T) {
	t.Parallel()

	var (
		adCalled    atomic.Bool
		azureCalled atomic.Bool
		adSentinel  = errors.New("ad post failure")
		azSentinel  = errors.New("azure post failure")
	)

	_, _, adErr, azureErr := runPostProcessingConcurrently(
		context.Background(),
		newErrorFn(&adCalled, adSentinel),
		newErrorFn(&azureCalled, azSentinel),
	)

	require.True(t, adCalled.Load(), "ad.Post should have been called")
	require.True(t, azureCalled.Load(), "azure.Post should have been called")
	require.ErrorIs(t, adErr, adSentinel, "ad error must be the ad sentinel")
	require.ErrorIs(t, azureErr, azSentinel, "azure error must be the azure sentinel")
}

// TestRunPostProcessingConcurrently_TrulyConcurrent verifies that both post functions
// run at the same time by checking that two functions each sleeping 50 ms complete in
// well under 150 ms total (they must overlap, not run sequentially).
func TestRunPostProcessingConcurrently_TrulyConcurrent(t *testing.T) {
	t.Parallel()

	const sleepDuration = 50 * time.Millisecond

	slowFn := func(_ context.Context) (*analysis.AtomicPostProcessingStats, error) {
		time.Sleep(sleepDuration)
		s := analysis.NewAtomicPostProcessingStats()
		return &s, nil
	}

	start := time.Now()
	_, _, adErr, azureErr := runPostProcessingConcurrently(context.Background(), slowFn, slowFn)
	elapsed := time.Since(start)

	require.NoError(t, adErr)
	require.NoError(t, azureErr)

	// If they ran sequentially, elapsed would be >= 2*sleepDuration (100 ms).
	// Concurrent execution must finish in under 1.5x the single sleep duration.
	assert.Less(t, elapsed, 3*sleepDuration/2,
		"AD and Azure post-processing must run concurrently, not sequentially (elapsed=%s)", elapsed)
}
