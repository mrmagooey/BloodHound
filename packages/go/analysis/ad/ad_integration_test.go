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

//go:build serial_integration

package ad_test

import (
	"context"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/test/integration"
	"github.com/specterops/bloodhound/packages/go/analysis/ad/internal/nodeprops"
	"github.com/specterops/bloodhound/packages/go/analysis/ad/wellknown"
	"github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/bloodhound/packages/go/graphschema/ad"
	"github.com/specterops/bloodhound/packages/go/graphschema/common"
	"github.com/specterops/dawgs/cypher/models/cypher"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"

	adAnalysis "github.com/specterops/bloodhound/packages/go/analysis/ad"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type linkWellKnownNodesTestCase struct {
	name      string
	setupFunc func(
		t *testing.T,
		ctx context.Context,
		graphDB graph.Database,
	) *graph.Node
	assertionFunc func(
		t *testing.T,
		ctx context.Context,
		graphDB graph.Database,
		expectedNode *graph.Node,
	)
}

func TestLinkWellKnownNodes(t *testing.T) {
	wellKnownNodes := []struct {
		sidSuffix      wellknown.SIDSuffix
		nodeNamePrefix wellknown.NodeNamePrefix
		kind           graph.Kind
	}{
		{
			sidSuffix:      wellknown.DomainUsersSIDSuffix,
			nodeNamePrefix: wellknown.DomainUsersNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.AuthenticatedUsersSIDSuffix,
			nodeNamePrefix: wellknown.AuthenticatedUsersNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.EveryoneSIDSuffix,
			nodeNamePrefix: wellknown.EveryoneNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.DomainComputersSIDSuffix,
			nodeNamePrefix: wellknown.DomainComputerNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.GuestSIDSuffix,
			nodeNamePrefix: wellknown.GuestNodeNamePrefix,
			kind:           ad.User,
		},
		{
			sidSuffix:      wellknown.NetworkSIDSuffix,
			nodeNamePrefix: wellknown.NetworkNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.ThisOrganizationSIDSuffix,
			nodeNamePrefix: wellknown.ThisOrganizationNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.ThisOrganizationCertificateSIDSuffix,
			nodeNamePrefix: wellknown.ThisOrganizationCertificateNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.AuthenticationAuthorityAssertedIdentitySIDSuffix,
			nodeNamePrefix: wellknown.AuthenticationAuthorityAssertedIdentityNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.KeyTrustSIDSuffix,
			nodeNamePrefix: wellknown.KeyTrustNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.MFAKeyPropertySIDSuffix,
			nodeNamePrefix: wellknown.MFAKeyPropertyNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.NTLMAuthenticationSIDSuffix,
			nodeNamePrefix: wellknown.NTLMAuthenticationNodeNamePrefix,
			kind:           ad.Group,
		},
		{
			sidSuffix:      wellknown.SchannelAuthenticationSIDSuffix,
			nodeNamePrefix: wellknown.SchannelAuthenticationNodeNamePrefix,
			kind:           ad.Group,
		},
	}
	wellKnownNodesDomain1 := make(map[wellknown.NodeNamePrefix]*graph.Node)
	wellKnownNodesDomain2 := make(map[wellknown.NodeNamePrefix]*graph.Node)
	wellKnownNodesDomain3 := make(map[wellknown.NodeNamePrefix]*graph.Node)

	testCases := []linkWellKnownNodesTestCase{
		{
			name: "Verifies that linking all well-known groups succeeds when they already exist.",
			setupFunc: func(
				t *testing.T,
				ctx context.Context,
				graphDB graph.Database,
			) *graph.Node {
				// NOTE: Testing the scenario requires created the wellknown groups ahead of time asserting their
				// execution when asserting the scenario prior to asserting the expected outcome for
				// LinkWellKnownNodes
				domain1Node := createNode(
					t,
					ctx,
					graphDB,
					generateCollectedDomain("DOMAIN1.LOCAL", "S-1-5-21-1004336348-1177238915-682003330"),
				)
				domain2Node := createNode(
					t,
					ctx,
					graphDB,
					generateCollectedDomain("DOMAIN2.LOCAL", "S-1-5-21-1004336348-1177238915-682003331"),
				)
				domain3Node := createNode(
					t,
					ctx,
					graphDB,
					generateCollectedDomain("DOMAIN3.LOCAL", "S-1-5-21-1004336348-1177238915-682003332"),
				)
				for _, wellKnownNode := range wellKnownNodes {
					// Domain1
					createdWellKnownNode := createNode(
						t,
						ctx,
						graphDB,
						generateWellKnownNode(
							t,
							domain1Node,
							wellKnownNode.sidSuffix,
							wellKnownNode.nodeNamePrefix,
							wellKnownNode.kind,
						),
					)
					wellKnownNodesDomain1[wellKnownNode.nodeNamePrefix] = createdWellKnownNode
					assertNodeExists(
						t,
						ctx,
						graphDB,
						createdWellKnownNode,
						query.StringEndsWith(
							query.NodeProperty(common.ObjectID.String()),
							wellKnownNode.sidSuffix.String(),
						),
						query.StringStartsWith(
							query.NodeProperty(common.Name.String()),
							wellKnownNode.nodeNamePrefix.String(),
						),
					)

					// Domain2
					createdWellKnownNode2 := createNode(
						t,
						ctx,
						graphDB,
						generateWellKnownNode(
							t,
							domain2Node,
							wellKnownNode.sidSuffix,
							wellKnownNode.nodeNamePrefix,
							wellKnownNode.kind,
						),
					)
					wellKnownNodesDomain2[wellKnownNode.nodeNamePrefix] = createdWellKnownNode2
					assertNodeExists(
						t,
						ctx,
						graphDB,
						createdWellKnownNode2,
						query.StringEndsWith(
							query.NodeProperty(common.ObjectID.String()),
							wellKnownNode.sidSuffix.String(),
						),
						query.StringStartsWith(
							query.NodeProperty(common.Name.String()),
							wellKnownNode.nodeNamePrefix.String(),
						),
					)

					// Domain3
					createdWellKnownNode3 := createNode(
						t,
						ctx,
						graphDB,
						generateWellKnownNode(
							t,
							domain3Node,
							wellKnownNode.sidSuffix,
							wellKnownNode.nodeNamePrefix,
							wellKnownNode.kind,
						),
					)
					wellKnownNodesDomain3[wellKnownNode.nodeNamePrefix] = createdWellKnownNode3
					assertNodeExists(
						t,
						ctx,
						graphDB,
						createdWellKnownNode3,
						query.StringEndsWith(
							query.NodeProperty(common.ObjectID.String()),
							wellKnownNode.sidSuffix.String(),
						),
						query.StringStartsWith(
							query.NodeProperty(common.Name.String()),
							wellKnownNode.nodeNamePrefix.String(),
						),
					)
				}

				//Trust edges
				err := createEdge(t, ctx, graphDB, domain2Node, domain1Node, ad.SameForestTrust)
				require.NoError(t, err)
				err = createEdge(t, ctx, graphDB, domain3Node, domain1Node, ad.CrossForestTrust)
				require.NoError(t, err)

				return domain1Node
			},
			assertionFunc: func(
				t *testing.T,
				ctx context.Context,
				graphDB graph.Database,
				expectedNode *graph.Node,
			) {
				// assert that the relationships exist
				expectationsMemberOfDomain1 := map[string][]*graph.Node{
					"domain users node is linked to authenticated users node": {
						wellKnownNodesDomain1[wellknown.DomainUsersNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.AuthenticatedUsersNodeNamePrefix],
					},
					"domain computers node is linked to authenticated users node": {
						wellKnownNodesDomain1[wellknown.DomainComputerNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.AuthenticatedUsersNodeNamePrefix],
					},
					"authenticated users node is linked to everyone node": {
						wellKnownNodesDomain1[wellknown.AuthenticatedUsersNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
					},
					"guest node is linked to everyone node": {
						wellKnownNodesDomain1[wellknown.GuestNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
					},
				}
				expectationsClaimSpecialIdentityDomain1 := map[string][]*graph.Node{
					"everyone node is linked to network node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.NetworkNodeNamePrefix],
					},
					"everyone node is linked to this organization node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.ThisOrganizationNodeNamePrefix],
					},
					"everyone node is linked to this organization certificate node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.ThisOrganizationCertificateNodeNamePrefix],
					},
					"everyone node is linked to authentication authority asserted identity node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.AuthenticationAuthorityAssertedIdentityNodeNamePrefix],
					},
					"everyone node is linked to key trust node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.KeyTrustNodeNamePrefix],
					},
					"everyone node is linked to mfa key property node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.MFAKeyPropertyNodeNamePrefix],
					},
					"everyone node is linked to ntlm authentication node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.NTLMAuthenticationNodeNamePrefix],
					},
					"everyone node is linked to schannel authentication node": {
						wellKnownNodesDomain1[wellknown.EveryoneNodeNamePrefix],
						wellKnownNodesDomain1[wellknown.SchannelAuthenticationNodeNamePrefix],
					},
				}

				for name, nodes := range expectationsMemberOfDomain1 {
					var expectedRelationship *graph.Relationship
					err := graphDB.ReadTransaction(ctx, func(tx graph.Transaction) error {
						rel, err := tx.Relationships().Filterf(func() graph.Criteria {
							return query.And(
								query.Equals(query.StartID(), nodes[0].ID),
								query.Equals(query.EndID(), nodes[1].ID),
								query.Kind(query.Relationship(), ad.MemberOf),
							)
						}).First()
						expectedRelationship = rel
						return err
					})
					require.NoError(t, err, name)
					require.NotNil(t, expectedRelationship, name)
				}
				for name, nodes := range expectationsClaimSpecialIdentityDomain1 {
					var expectedRelationship *graph.Relationship
					err := graphDB.ReadTransaction(ctx, func(tx graph.Transaction) error {
						rel, err := tx.Relationships().Filterf(func() graph.Criteria {
							return query.And(
								query.Equals(query.StartID(), nodes[0].ID),
								query.Equals(query.EndID(), nodes[1].ID),
								query.Kind(query.Relationship(), ad.ClaimSpecialIdentity),
							)
						}).First()
						expectedRelationship = rel
						return err
					})
					require.NoError(t, err, name)
					require.NotNil(t, expectedRelationship, name)
				}
			},
		},
	}

	// Run tests for each database driver
	ctx := context.Background()
	// Initialize the database
	graphDB := integration.OpenGraphDB(t, graphschema.DefaultGraphSchema())
	defer graphDB.Close(ctx)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			createdNode := tc.setupFunc(
				t,
				ctx,
				graphDB,
			)
			require.NotNil(t, createdNode)

			// Run LinkWellKnownNodes
			err := adAnalysis.LinkWellKnownNodes(ctx, graphDB)
			require.NoError(t, err)

			tc.assertionFunc(t, ctx, graphDB, createdNode)
			require.NoError(t, err)
		})
	}
}

func fetchNode(
	t *testing.T,
	ctx context.Context,
	graphDB graph.Database,
	cypherComparisons ...*cypher.Comparison,
) (
	*graph.Node,
	error,
) {
	t.Helper()

	var fetchedNode *graph.Node
	err := graphDB.ReadTransaction(ctx, func(tx graph.Transaction) error {
		filteredNode, err := tx.Nodes().Filterf(func() graph.Criteria {
			graphCriterias := make([]graph.Criteria, len(cypherComparisons))
			for i := range cypherComparisons {
				graphCriterias[i] = cypherComparisons[i]
			}
			return query.And(graphCriterias...)
		}).First()
		if err != nil {
			return err
		}
		fetchedNode = filteredNode

		return nil
	})
	return fetchedNode, err
}

func assertNodeExists(
	t *testing.T,
	ctx context.Context,
	graphDB graph.Database,
	expectedNode *graph.Node,
	queryCriterias ...*cypher.Comparison,
) {
	t.Helper()

	domainSID, err := expectedNode.Properties.Get(ad.DomainSID.String()).String()
	require.NoError(t, err)
	domainFQDN, err := expectedNode.Properties.Get(ad.DomainFQDN.String()).String()
	require.NoError(t, err)
	nodeName, err := expectedNode.Properties.Get(common.Name.String()).String()
	require.NoError(t, err)

	defaultQueryCriterias := []*cypher.Comparison{
		query.Equals(query.NodeProperty(ad.DomainSID.String()), domainSID),
		query.Equals(query.NodeProperty(ad.DomainFQDN.String()), domainFQDN),
		query.Equals(query.NodeProperty(common.Name.String()), nodeName),
	}

	queryCriterias = append(queryCriterias, defaultQueryCriterias...)
	fetchedNode, err := fetchNode(t, ctx, graphDB, queryCriterias...)
	require.NoError(t, err)
	require.NotNil(t, fetchedNode)
}

func createNode(
	t *testing.T,
	ctx context.Context,
	graphDB graph.Database,
	nodeToCreate *graph.Node,
) *graph.Node {
	t.Helper()
	require.NotNil(t, graphDB)
	require.NotNil(t, nodeToCreate)

	if ctx == nil {
		ctx = context.Background()
	}

	var createdNode *graph.Node
	var err error
	err = graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		createdNode, err = tx.CreateNode(nodeToCreate.Properties, nodeToCreate.Kinds...)
		if err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, createdNode)

	assertNodeExists(t, ctx, graphDB, createdNode)
	return createdNode
}

func createEdge(
	t *testing.T,
	ctx context.Context,
	graphDB graph.Database,
	startNode *graph.Node,
	endNode *graph.Node,
	edgeKind graph.Kind,
) error {
	t.Helper()
	require.NotNil(t, graphDB)
	require.NotNil(t, startNode)
	require.NotNil(t, endNode)

	if ctx == nil {
		ctx = context.Background()
	}

	var createdEdge *graph.Relationship
	var err error
	err = graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		createdEdge, err = tx.CreateRelationshipByIDs(startNode.ID, endNode.ID, edgeKind, graph.NewProperties())
		if err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, createdEdge)

	return nil
}

func generateCollectedDomain(name, sid string) *graph.Node {
	return &graph.Node{
		Kinds: graph.Kinds{
			ad.Domain, ad.Entity,
		},
		Properties: graph.AsProperties(graph.PropertyMap{
			common.Collected: true,
			common.Name:      name,
			ad.DomainSID:     sid,
			ad.DomainFQDN:    name,
		}),
	}
}

func generateWellKnownNode(
	t *testing.T,
	domainNode *graph.Node,
	sidSuffix wellknown.SIDSuffix,
	nodeNamePrefix wellknown.NodeNamePrefix,
	nodeKind graph.Kind,
) *graph.Node {
	require.NotNil(t, domainNode)
	require.NotNil(t, sidSuffix)
	require.NotNil(t, nodeNamePrefix)

	domainSID, domainName, err := nodeprops.ReadDomainIDandNameAsString(domainNode)
	require.NoError(t, err)

	var wellKnownSID string
	switch sidSuffix.String() {
	case wellknown.DomainUsersSIDSuffix.String(), wellknown.DomainComputersSIDSuffix.String(), wellknown.GuestSIDSuffix.String():
		wellKnownSID = sidSuffix.PrependPrefix(domainSID)
	default:
		wellKnownSID = sidSuffix.PrependPrefix(domainName)
	}

	return &graph.Node{
		Kinds: graph.Kinds{
			ad.Entity,
			nodeKind,
		},
		Properties: graph.AsProperties(graph.PropertyMap{
			ad.DomainFQDN:    domainName,
			ad.DomainSID:     domainSID,
			common.Collected: true,
			common.Name:      nodeNamePrefix.AppendSuffix(domainName),
			common.ObjectID:  wellKnownSID,
		}),
	}
}

// ─── OPT-23 integration tests ──────────────────────────────────────────────────

// TestFetchWellKnownTierZeroEntities_AllSuffixesReturned verifies that the combined
// OR query (OPT-23) returns a node for every tier-zero SID suffix that exists in the
// database, matching what the original per-suffix loop would have returned.
func TestFetchWellKnownTierZeroEntities_AllSuffixesReturned(t *testing.T) {
	ctx := context.Background()
	graphDB := integration.OpenGraphDB(t, graphschema.DefaultGraphSchema())
	defer graphDB.Close(ctx)

	const (
		domainName = "OPTZERO.LOCAL"
		domainSID  = "S-1-5-21-100000001-200000001-300000001"
	)

	// Create a domain node.
	var domainNode *graph.Node
	require.NoError(t, graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		var err error
		domainNode, err = tx.CreateNode(graph.AsProperties(graph.PropertyMap{
			common.Collected: true,
			common.Name:      domainName,
			ad.DomainSID:     domainSID,
			ad.DomainFQDN:    domainName,
		}), ad.Entity, ad.Domain)
		return err
	}))
	require.NotNil(t, domainNode)

	// Create one Group node per tier-zero SID suffix.
	suffixes := adAnalysis.TierZeroWellKnownSIDSuffixes()
	createdNodeIDs := make(map[graph.ID]struct{}, len(suffixes))

	require.NoError(t, graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for _, suffix := range suffixes {
			node, err := tx.CreateNode(graph.AsProperties(graph.PropertyMap{
				common.ObjectID: domainSID + suffix,
				ad.DomainSID:    domainSID,
			}), ad.Entity, ad.Group)
			if err != nil {
				return err
			}
			createdNodeIDs[node.ID] = struct{}{}
		}
		return nil
	}))

	// Add an irrelevant node from a different domain — must NOT be returned.
	require.NoError(t, graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.AsProperties(graph.PropertyMap{
			common.ObjectID: "S-1-5-21-999999999-999999999-999999999" + suffixes[0],
			ad.DomainSID:    "S-1-5-21-999999999-999999999-999999999",
		}), ad.Entity, ad.Group)
		return err
	}))

	// Add an AdminSDHolder Container for this domain.
	var adminSDHolderID graph.ID
	require.NoError(t, graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		node, err := tx.CreateNode(graph.AsProperties(graph.PropertyMap{
			common.ObjectID:      domainSID + "-ADMINSDHOLDER",
			ad.DistinguishedName: adAnalysis.AdminSDHolderDNPrefix + "DC=OPTZERO,DC=LOCAL",
			ad.DomainSID:         domainSID,
		}), ad.Entity, ad.Container)
		if err != nil {
			return err
		}
		adminSDHolderID = node.ID
		return nil
	}))

	// Exercise the function under test.
	result, err := adAnalysis.FetchWellKnownTierZeroEntities(ctx, graphDB, domainSID)
	require.NoError(t, err)

	// All tier-zero SID-suffix nodes must be present.
	for id := range createdNodeIDs {
		assert.True(t, result.ContainsID(id), "expected node %d to be in result", id)
	}
	// AdminSDHolder must be present.
	assert.True(t, result.ContainsID(adminSDHolderID), "expected AdminSDHolder node in result")

	// Total count: one per suffix + AdminSDHolder.
	assert.Equal(t, len(suffixes)+1, result.Len(),
		"result should contain exactly one node per suffix plus AdminSDHolder")
}

// TestFetchWellKnownTierZeroEntities_ExcludesWrongDomain verifies that nodes
// belonging to a different domain are not returned even if they share SID suffixes.
func TestFetchWellKnownTierZeroEntities_ExcludesWrongDomain(t *testing.T) {
	ctx := context.Background()
	graphDB := integration.OpenGraphDB(t, graphschema.DefaultGraphSchema())
	defer graphDB.Close(ctx)

	const targetDomainSID = "S-1-5-21-777000001-777000002-777000003"
	const otherDomainSID  = "S-1-5-21-888000001-888000002-888000003"

	// Create one tier-zero node in the target domain and one in another domain.
	suffix := adAnalysis.TierZeroWellKnownSIDSuffixes()[0]
	var targetNodeID graph.ID
	require.NoError(t, graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		target, err := tx.CreateNode(graph.AsProperties(graph.PropertyMap{
			common.ObjectID: targetDomainSID + suffix,
			ad.DomainSID:    targetDomainSID,
		}), ad.Entity, ad.Group)
		if err != nil {
			return err
		}
		targetNodeID = target.ID

		_, err = tx.CreateNode(graph.AsProperties(graph.PropertyMap{
			common.ObjectID: otherDomainSID + suffix,
			ad.DomainSID:    otherDomainSID,
		}), ad.Entity, ad.Group)
		return err
	}))

	result, err := adAnalysis.FetchWellKnownTierZeroEntities(ctx, graphDB, targetDomainSID)
	require.NoError(t, err)

	require.Equal(t, 1, result.Len(), "only the target-domain node should be returned")
	assert.NotNil(t, result.Get(targetNodeID))
}

// TestFetchWellKnownTierZeroEntities_ExcludesNonGroupUser verifies that nodes
// that match a SID suffix but lack Group or User kind are not returned.
func TestFetchWellKnownTierZeroEntities_ExcludesNonGroupUser(t *testing.T) {
	ctx := context.Background()
	graphDB := integration.OpenGraphDB(t, graphschema.DefaultGraphSchema())
	defer graphDB.Close(ctx)

	const domainSID = "S-1-5-21-666000001-666000002-666000003"
	suffix := adAnalysis.TierZeroWellKnownSIDSuffixes()[0]

	// Create a Computer node with a matching SID — must NOT be returned.
	require.NoError(t, graphDB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.AsProperties(graph.PropertyMap{
			common.ObjectID: domainSID + suffix,
			ad.DomainSID:    domainSID,
		}), ad.Entity, ad.Computer)
		return err
	}))

	result, err := adAnalysis.FetchWellKnownTierZeroEntities(ctx, graphDB, domainSID)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len(), "Computer node with tier-zero SID suffix should be excluded")
}


