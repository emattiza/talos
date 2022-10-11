// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration_api

package api

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/gen/maps"
	"github.com/siderolabs/gen/slices"
	"github.com/siderolabs/gen/value"
	"go4.org/netipx"

	"github.com/talos-systems/talos/internal/integration/base"
	"github.com/talos-systems/talos/pkg/machinery/client"
	"github.com/talos-systems/talos/pkg/machinery/resources/cluster"
	"github.com/talos-systems/talos/pkg/machinery/resources/kubespan"
)

// DiscoverySuite verifies Discovery API.
type DiscoverySuite struct {
	base.APISuite

	ctx       context.Context //nolint:containedctx
	ctxCancel context.CancelFunc
}

// SuiteName ...
func (suite *DiscoverySuite) SuiteName() string {
	return "api.DiscoverySuite"
}

// SetupTest ...
func (suite *DiscoverySuite) SetupTest() {
	// make sure API calls have timeout
	suite.ctx, suite.ctxCancel = context.WithTimeout(context.Background(), 15*time.Second)

	// check that cluster has discovery enabled
	node := suite.RandomDiscoveredNodeInternalIP()
	suite.ClearConnectionRefused(suite.ctx, node)

	nodeCtx := client.WithNodes(suite.ctx, node)
	provider, err := suite.ReadConfigFromNode(nodeCtx)
	suite.Require().NoError(err)

	if !provider.Cluster().Discovery().Enabled() {
		suite.T().Skip("cluster discovery is disabled")
	}
}

// TearDownTest ...
func (suite *DiscoverySuite) TearDownTest() {
	if suite.ctxCancel != nil {
		suite.ctxCancel()
	}
}

// TestMembers checks that `talosctl get members` matches expected cluster discovery.
//
//nolint:gocyclo
func (suite *DiscoverySuite) TestMembers() {
	nodes := suite.DiscoverNodes(suite.ctx).Nodes()

	expectedTalosVersion := fmt.Sprintf("Talos (%s)", suite.Version)

	for _, node := range nodes {
		nodeCtx := client.WithNode(suite.ctx, node.InternalIP.String())

		members := suite.getMembers(nodeCtx)

		suite.Assert().Len(members, len(nodes))

		// do basic check against discovered nodes
		for _, expectedNode := range nodes {
			nodeAddresses := slices.Map(expectedNode.IPs, func(t netip.Addr) string {
				return t.String()
			})

			found := false

			for _, member := range members {
				memberAddresses := slices.Map(member.TypedSpec().Addresses, func(t netip.Addr) string {
					return t.String()
				})

				if maps.Contains(slices.ToSet(memberAddresses), nodeAddresses) {
					found = true

					break
				}

				if found {
					break
				}
			}

			suite.Assert().True(found, "addr %q", nodeAddresses)
		}

		// if cluster information is available, perform additional checks
		if suite.Cluster == nil {
			continue
		}

		memberByName := slices.ToMap(members,
			func(member *cluster.Member) (string, *cluster.Member) {
				return member.Metadata().ID(), member
			},
		)

		memberByIP := make(map[netip.Addr]*cluster.Member)

		for _, member := range members {
			for _, addr := range member.TypedSpec().Addresses {
				memberByIP[addr] = member
			}
		}

		nodesInfo := suite.Cluster.Info().Nodes

		for _, nodeInfo := range nodesInfo {
			matchingMember := memberByName[nodeInfo.Name]

			var matchingMemberByIP *cluster.Member

			for _, nodeIPStd := range nodeInfo.IPs {
				nodeIP, ok := netipx.FromStdIP(nodeIPStd)
				suite.Assert().True(ok)

				matchingMemberByIP = memberByIP[nodeIP]

				break
			}

			// if hostnames are not set via DHCP, use match by IP
			if matchingMember == nil {
				matchingMember = matchingMemberByIP
			}

			suite.Require().NotNil(matchingMember)

			suite.Assert().Equal(nodeInfo.Type, matchingMember.TypedSpec().MachineType)
			suite.Assert().Equal(expectedTalosVersion, matchingMember.TypedSpec().OperatingSystem)

			for _, nodeIPStd := range nodeInfo.IPs {
				nodeIP, ok := netipx.FromStdIP(nodeIPStd)
				suite.Assert().True(ok)

				found := false

				for _, memberAddr := range matchingMember.TypedSpec().Addresses {
					if memberAddr.Compare(nodeIP) == 0 {
						found = true

						break
					}
				}

				suite.Assert().True(found, "addr %s", nodeIP)
			}
		}
	}
}

// TestRegistries checks that all registries produce same raw Affiliate data.
func (suite *DiscoverySuite) TestRegistries() {
	node := suite.RandomDiscoveredNodeInternalIP()
	suite.ClearConnectionRefused(suite.ctx, node)

	nodeCtx := client.WithNodes(suite.ctx, node)
	provider, err := suite.ReadConfigFromNode(nodeCtx)
	suite.Require().NoError(err)

	var registries []string

	if provider.Cluster().Discovery().Registries().Kubernetes().Enabled() {
		registries = append(registries, "k8s/")
	}

	if provider.Cluster().Discovery().Registries().Service().Enabled() {
		registries = append(registries, "service/")
	}

	nodes := suite.DiscoverNodeInternalIPs(suite.ctx)

	for _, node := range nodes {
		nodeCtx := client.WithNode(suite.ctx, node)

		members := suite.getMembers(nodeCtx)
		localIdentity := suite.getNodeIdentity(nodeCtx)

		// raw affiliates don't contain the local node
		expectedRawAffiliates := len(registries) * (len(members) - 1)

		var rawAffiliates []*cluster.Affiliate

		for i := 0; i < 30; i++ {
			rawAffiliates = suite.getAffiliates(nodeCtx, cluster.RawNamespaceName)

			if len(rawAffiliates) == expectedRawAffiliates {
				break
			}

			suite.T().Logf("waiting for cluster affiliates to be discovered: %d expected, %d found", expectedRawAffiliates, len(rawAffiliates))

			time.Sleep(2 * time.Second)
		}

		suite.Assert().Len(rawAffiliates, expectedRawAffiliates)

		rawAffiliatesByID := make(map[string]*cluster.Affiliate)

		for _, rawAffiliate := range rawAffiliates {
			rawAffiliatesByID[rawAffiliate.Metadata().ID()] = rawAffiliate
		}

		// every member except for local identity member should be discovered via each registry
		for _, member := range members {
			if member.TypedSpec().NodeID == localIdentity.TypedSpec().NodeID {
				continue
			}

			for _, registry := range registries {
				rawAffiliate := rawAffiliatesByID[registry+member.TypedSpec().NodeID]
				suite.Require().NotNil(rawAffiliate)

				stripDomain := func(s string) string { return strings.Split(s, ".")[0] }

				// registries can be a bit inconsistent, e.g. whether they report fqdn or just hostname
				suite.Assert().Contains([]string{member.TypedSpec().Hostname, stripDomain(member.TypedSpec().Hostname)}, rawAffiliate.TypedSpec().Hostname)

				suite.Assert().Equal(member.TypedSpec().Addresses, rawAffiliate.TypedSpec().Addresses)
				suite.Assert().Equal(member.TypedSpec().OperatingSystem, rawAffiliate.TypedSpec().OperatingSystem)
				suite.Assert().Equal(member.TypedSpec().MachineType, rawAffiliate.TypedSpec().MachineType)
			}
		}
	}
}

// TestKubeSpanPeers verifies that KubeSpan peer specs are populated, and that peer statuses are available.
func (suite *DiscoverySuite) TestKubeSpanPeers() {
	if !suite.Capabilities().RunsTalosKernel {
		suite.T().Skip("not running Talos kernel")
	}

	// check that cluster has KubeSpan enabled
	node := suite.RandomDiscoveredNodeInternalIP()
	suite.ClearConnectionRefused(suite.ctx, node)

	nodeCtx := client.WithNode(suite.ctx, node)
	provider, err := suite.ReadConfigFromNode(nodeCtx)
	suite.Require().NoError(err)

	if !provider.Machine().Network().KubeSpan().Enabled() {
		suite.T().Skip("KubeSpan is disabled")
	}

	nodes := suite.DiscoverNodeInternalIPs(suite.ctx)

	for _, node := range nodes {
		nodeCtx := client.WithNode(suite.ctx, node)

		peerSpecs := suite.getKubeSpanPeerSpecs(nodeCtx)
		suite.Assert().Len(peerSpecs, len(nodes)-1)

		peerStatuses := suite.getKubeSpanPeerStatuses(nodeCtx)
		suite.Assert().Len(peerStatuses, len(nodes)-1)

		for _, status := range peerStatuses {
			suite.Assert().Equal(kubespan.PeerStateUp, status.TypedSpec().State)
			suite.Assert().False(value.IsZero(status.TypedSpec().Endpoint))
			suite.Assert().Greater(status.TypedSpec().ReceiveBytes, int64(0))
			suite.Assert().Greater(status.TypedSpec().TransmitBytes, int64(0))
		}
	}
}

//nolint:dupl
func (suite *DiscoverySuite) getMembers(nodeCtx context.Context) []*cluster.Member {
	var result []*cluster.Member

	items, err := safe.StateList[*cluster.Member](nodeCtx, suite.Client.COSI, resource.NewMetadata(cluster.NamespaceName, cluster.MemberType, "", resource.VersionUndefined))
	suite.Require().NoError(err)

	it := safe.IteratorFromList(items)

	for it.Next() {
		result = append(result, it.Value())
	}

	return result
}

func (suite *DiscoverySuite) getNodeIdentity(nodeCtx context.Context) *cluster.Identity {
	identity, err := safe.StateGet[*cluster.Identity](nodeCtx, suite.Client.COSI, resource.NewMetadata(cluster.NamespaceName, cluster.IdentityType, cluster.LocalIdentity, resource.VersionUndefined))
	suite.Require().NoError(err)

	return identity
}

//nolint:dupl
func (suite *DiscoverySuite) getAffiliates(nodeCtx context.Context, namespace resource.Namespace) []*cluster.Affiliate {
	var result []*cluster.Affiliate

	items, err := safe.StateList[*cluster.Affiliate](nodeCtx, suite.Client.COSI, resource.NewMetadata(namespace, cluster.AffiliateType, "", resource.VersionUndefined))
	suite.Require().NoError(err)

	it := safe.IteratorFromList(items)

	for it.Next() {
		result = append(result, it.Value())
	}

	return result
}

//nolint:dupl
func (suite *DiscoverySuite) getKubeSpanPeerSpecs(nodeCtx context.Context) []*kubespan.PeerSpec {
	var result []*kubespan.PeerSpec

	items, err := safe.StateList[*kubespan.PeerSpec](nodeCtx, suite.Client.COSI, resource.NewMetadata(kubespan.NamespaceName, kubespan.PeerSpecType, "", resource.VersionUndefined))
	suite.Require().NoError(err)

	it := safe.IteratorFromList(items)

	for it.Next() {
		result = append(result, it.Value())
	}

	return result
}

//nolint:dupl
func (suite *DiscoverySuite) getKubeSpanPeerStatuses(nodeCtx context.Context) []*kubespan.PeerStatus {
	var result []*kubespan.PeerStatus

	items, err := safe.StateList[*kubespan.PeerStatus](nodeCtx, suite.Client.COSI, resource.NewMetadata(kubespan.NamespaceName, kubespan.PeerStatusType, "", resource.VersionUndefined))
	suite.Require().NoError(err)

	it := safe.IteratorFromList(items)

	for it.Next() {
		result = append(result, it.Value())
	}

	return result
}

func init() {
	allSuites = append(allSuites, new(DiscoverySuite))
}
