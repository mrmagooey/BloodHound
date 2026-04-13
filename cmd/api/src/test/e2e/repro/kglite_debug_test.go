//go:build e2e

package repro

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/analysis/ad"
	"github.com/specterops/bloodhound/cmd/api/src/analysis/azure"
	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/stretchr/testify/require"
)

func TestKNexusDuplicateDetails(t *testing.T) {
	ctx := context.Background()
	zipPath := knexusDataPath()

	db := openGraph(t)
	t.Log("Ingesting...")
	ingestZipFull(ctx, t, db, zipPath)

	t.Log("Running analysis...")
	counter := analysis.NewCompositionCounter()
	_, err := ad.Post(ctx, db, true, false, true, &counter)
	require.NoError(t, err)
	_, err = azure.Post(ctx, db)
	require.NoError(t, err)

	// Get the 5 most duplicated OIDs
	dupeOids := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN oid, cnt ORDER BY cnt DESC LIMIT 5")

	t.Log("=== Duplicate OID node details (id, labels, name per physical node) ===")
	for _, line := range strings.Split(dupeOids, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ", ", 2)
		if len(parts) < 2 {
			continue
		}
		oid := parts[0]
		cnt := parts[1]
		t.Logf("OID=%s cnt=%s", oid, cnt)

		// Get ALL physical nodes with this objectid, differentiated by internal id
		rows := queryString(ctx, t, db,
			fmt.Sprintf("MATCH (n) WHERE n.objectid = '%s' RETURN id(n), labels(n), n.name", oid))
		for _, r := range strings.Split(rows, "\n") {
			if r != "" {
				t.Logf("  node: %s", r)
			}
		}
	}

	// Also check label distribution among duplicates
	t.Log("")
	t.Log("=== Label distribution among all duplicate nodes ===")
	labelDist := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 MATCH (n) WHERE n.objectid = oid UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")
	for _, r := range strings.Split(labelDist, "\n") {
		if r != "" {
			t.Logf("  %s", r)
		}
	}

	total := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("Total nodes: %d", total)
}
