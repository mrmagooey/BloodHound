//go:build e2e

// Localizes the residual +9 SCIM_Group divergence between kglite and Neo4j
// in the KNexus comparison test. Reads the KNexus zip and ingests ONLY the
// files that touch the 9 SCIM_Group OIDs: 04-oktahound/{okta-graph,preview2-okta-graph}.json
// and 06-githound/{githound_enterprise_E,githound_enterprise_scim_E}.json,
// in zip order. Then prints the post-ingest state of those 9 OIDs and any
// nodes named like the Okta_Group "GitHub-All-Users" / "Corp-DevOps" / etc.
//
// Run with:
//
//	go test -v -tags e2e -timeout 10m -run TestSCIMOktaGroupResidual ./cmd/api/src/test/e2e/repro/
package repro

import (
	"archive/zip"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify/endpoint"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/bomenc"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// TestSCIMOktaGroupResidualFull does a full zip ingest and isolates the +9
// Okta_Group label-combo difference between kglite and Neo4j post-fix.
func TestSCIMOktaGroupResidualFull(t *testing.T) {
	ctx := context.Background()
	zipPath := knexusDataPath()
	db := openGraph(t)
	ingestZipFull(ctx, t, db, zipPath)

	t.Log("=== full ingest: Okta_Group label combos ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n:Okta_Group) RETURN labels(n) AS lbl, count(*) AS c ORDER BY c DESC"))

	t.Log("=== full ingest: state of the 9 SCIM_Group OIDs ===")
	scimGroupOIDs := []string{
		"35333665306166382D316534642D313166312D386436612D623362323565336237396435",
		"32643833663434362D316534652D313166312D383731322D663230636436326531396534",
		"33303962643766632D316534652D313166312D393763362D646238653036656331613664",
		"33336537363865302D316534652D313166312D383665322D636366643066336266643830",
		"33363937323462382D316534652D313166312D393164652D656664396564393333656439",
		"33393566613730362D316534652D313166312D386264372D313765633033323734613539",
		"33633937303032632D316534652D313166312D386562662D393532616633303539373736",
		"33663836346130342D316534652D313166312D386639342D393833663239336634646430",
		"34346265626561632D316534652D313166312D396334642D626461393736373636336163",
	}
	for _, oid := range scimGroupOIDs {
		rows := queryString(ctx, t, db,
			"MATCH (n {objectid: '"+oid+"'}) RETURN id(n), labels(n), n.name")
		t.Logf("  oid=%s..\n%s", oid[:14], rows)
	}

	t.Log("=== full ingest: top 30 most-duplicated objectids with labels ===")
	dupes := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 "+
			"RETURN oid, cnt ORDER BY cnt DESC LIMIT 30")
	for _, line := range strings.Split(dupes, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ", ", 2)
		oid := parts[0]
		labels := queryString(ctx, t, db,
			"MATCH (n {objectid: '"+oid+"'}) RETURN labels(n)")
		t.Logf("  oid=%s cnt=%s\n    %s", oid, parts[1], strings.ReplaceAll(labels, "\n", "\n    "))
	}

	t.Log("=== full ingest: any node with both Okta_Group and SCIM_Group? ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n) WHERE n:Okta_Group AND n:SCIM_Group RETURN n.objectid, labels(n)"))

	t.Log("=== full ingest: the 9 [SCIM Okta_Group] nodes (kglite-only) ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n:SCIM) WHERE n:Okta_Group RETURN id(n), labels(n), n.name, n.objectid"))

	t.Log("=== full ingest: incoming/outgoing rels for these 9 nodes ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n:SCIM)-[r]->(b) WHERE n:Okta_Group "+
			"RETURN n.objectid, type(r), labels(b), b.objectid LIMIT 50"))
	t.Log(queryString(ctx, t, db,
		"MATCH (a)-[r]->(n:SCIM) WHERE n:Okta_Group "+
			"RETURN labels(a), a.objectid, type(r), n.objectid LIMIT 50"))
}

func TestSCIMOktaGroupResidual(t *testing.T) {
	ctx := context.Background()
	zipPath := knexusDataPath()

	// 9 SCIM_Group OIDs identified from Neo4j as having labels [SCIM, SCIM_Group].
	scimGroupOIDs := []string{
		"35333665306166382D316534642D313166312D386436612D623362323565336237396435",
		"32643833663434362D316534652D313166312D383731322D663230636436326531396534",
		"33303962643766632D316534652D313166312D393763362D646238653036656331613664",
		"33336537363865302D316534652D313166312D383665322D636366643066336266643830",
		"33363937323462382D316534652D313166312D393164652D656664396564393333656439",
		"33393566613730362D316534652D313166312D386264372D313765633033323734613539",
		"33633937303032632D316534652D313166312D386562662D393532616633303539373736",
		"33663836346130342D316534652D313166312D386639342D393833663239336634646430",
		"34346265626561632D316534652D313166312D396334642D626461393736373636336163",
	}

	// The names referenced via SCIM_Provisioned start={match_by:name, kind:Okta_Group}.
	groupNames := []string{
		"GitHub-All-Users",
		"Corp-DevOps",
		"Corp-Security",
		"Engineering-All",
		"Corp-IT-Privileged",
		"IT-Cloud-Operations",
		"Product-Management",
		"IT-Security-Operations",
		"GitHub-Org-Owners",
	}

	// Files to ingest in zip order.
	wantFiles := map[string]bool{
		"k-nexusglobal-bhce-20260330/04-oktahound/okta-graph.json":                            true,
		"k-nexusglobal-bhce-20260330/04-oktahound/preview2-okta-graph.json":                   true,
		"k-nexusglobal-bhce-20260330/06-githound/githound_enterprise_E_kgDOAAiv9g.json":       true,
		"k-nexusglobal-bhce-20260330/06-githound/githound_enterprise_scim_E_kgDOAAiv9g.json":  true,
	}

	db := openGraph(t)

	schema, err := upload.LoadIngestSchema()
	require.NoError(t, err)
	resolver := endpoint.NewResolver(db)
	ic := graphify.NewIngestContext(ctx,
		graphify.WithIngestTime(time.Now().UTC()),
		graphify.WithEndpointResolver(resolver),
	)
	readOpts := graphify.ReadOptions{
		FileType:     model.FileTypeJson,
		IngestSchema: schema,
		RegisterSourceKind: func(kind graph.Kind) error {
			return db.RefreshKinds(ctx)
		},
	}

	// For visibility, ingest each file in its own BatchOperation so we can
	// observe state after each step.
	archive, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	defer archive.Close()

	type entry struct {
		name string
		data []byte
	}
	var ordered []entry
	for _, f := range archive.File {
		if !wantFiles[f.Name] {
			continue
		}
		src, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		norm, err := bomenc.NormalizeToUTF8(src)
		require.NoError(t, err)
		buf, err := io.ReadAll(norm)
		src.Close()
		require.NoError(t, err)
		ordered = append(ordered, entry{name: f.Name, data: buf})
	}
	t.Logf("ingesting %d files in zip order", len(ordered))

	for _, e := range ordered {
		t.Logf("=== ingesting %s (%d bytes) ===", e.name, len(e.data))
		err := db.BatchOperation(ctx, func(batch graph.Batch) error {
			ic.BindBatchUpdater(batch)
			tmp, err := os.CreateTemp("", "scim-residual-*")
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			if _, err := tmp.Write(e.data); err != nil {
				tmp.Close()
				return err
			}
			tmp.Seek(0, io.SeekStart)
			if err := graphify.ReadFileForIngest(ic, tmp, readOpts); err != nil {
				slog.Warn("ingest non-fatal", "file", e.name, "error", err)
			}
			tmp.Close()
			return nil
		})
		if err != nil {
			t.Logf("WARN: batch error %s: %v", e.name, err)
		}

		// Snapshot relevant state.
		total := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
		oktaGroupCount := queryCount(ctx, t, db, "MATCH (n:Okta_Group) RETURN count(n)")
		scimGroupCount := queryCount(ctx, t, db, "MATCH (n:SCIM_Group) RETURN count(n)")
		scimOktaGroupCount := queryCount(ctx, t, db,
			"MATCH (n:SCIM) WHERE n:Okta_Group RETURN count(n)")
		t.Logf("  total=%d Okta_Group=%d SCIM_Group=%d (SCIM&Okta_Group=%d)",
			total, oktaGroupCount, scimGroupCount, scimOktaGroupCount)
		t.Logf("  Okta_Group label combos:\n%s",
			queryString(ctx, t, db,
				"MATCH (n:Okta_Group) RETURN labels(n) AS lbl, count(*) AS c ORDER BY c DESC"))
	}

	t.Log("=== final per-label counts ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC"))

	t.Log("=== Okta_Group label combos ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n:Okta_Group) RETURN labels(n) AS lbl, count(*) AS c ORDER BY c DESC"))
	t.Log("=== SCIM_Group label combos ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n:SCIM_Group) RETURN labels(n) AS lbl, count(*) AS c ORDER BY c DESC"))

	t.Log("=== state of the 9 SCIM_Group OIDs in kglite ===")
	for _, oid := range scimGroupOIDs {
		rows := queryString(ctx, t, db,
			"MATCH (n {objectid: '"+oid+"'}) RETURN id(n), labels(n), n.name")
		if rows == "" {
			rows = "(no node)"
		}
		t.Logf("  oid=%s..\n%s", oid[:14], rows)
	}

	t.Log("=== Okta_Group nodes by name (case-insensitive, what match_by=name does) ===")
	for _, nm := range groupNames {
		rows := queryString(ctx, t, db,
			"MATCH (n:Okta_Group) WHERE toLower(n.name) = toLower('"+nm+"') "+
				"RETURN id(n), labels(n), n.name, n.objectid")
		t.Logf("  name=%q\n%s", nm, rows)
	}

	t.Log("=== SCIM_Provisioned edges where end-objectid is one of the 9 ===")
	for _, oid := range scimGroupOIDs {
		rows := queryString(ctx, t, db,
			"MATCH (a)-[r:SCIM_Provisioned]->(b {objectid: '"+oid+"'}) "+
				"RETURN id(a), labels(a), a.name, a.objectid, id(b), labels(b)")
		if rows == "" {
			continue
		}
		t.Logf("  oid=%s..\n%s", oid[:14], rows)
	}

	// Total nodes-per-objectid distribution for sanity.
	t.Log("=== node copies-per-objectid distribution ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"RETURN cnt AS copies, count(oid) AS num_oids ORDER BY cnt"))

	// Sanity: list any oids whose nodes carry both Okta_Group AND SCIM_Group labels.
	t.Log("=== any node carrying both Okta_Group AND SCIM_Group? ===")
	t.Log(queryString(ctx, t, db,
		"MATCH (n) WHERE n:Okta_Group AND n:SCIM_Group RETURN n.objectid, labels(n) LIMIT 20"))

	_ = strings.TrimSpace
}
