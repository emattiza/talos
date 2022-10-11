// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration_api

package api

import (
	"context"
	"net/url"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/talos-systems/talos/internal/integration/base"
	machineapi "github.com/talos-systems/talos/pkg/machinery/api/machine"
	"github.com/talos-systems/talos/pkg/machinery/client"
	"github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1/machine"
)

// UpdateEndpointSuite verifies UpdateEndpoint API.
type UpdateEndpointSuite struct {
	base.K8sSuite

	ctx       context.Context //nolint:containedctx
	ctxCancel context.CancelFunc
}

// SuiteName ...
func (suite *UpdateEndpointSuite) SuiteName() string {
	return "api.UpdateEndpointSuite"
}

// SetupTest ...
func (suite *UpdateEndpointSuite) SetupTest() {
	// make sure API calls have timeout
	suite.ctx, suite.ctxCancel = context.WithTimeout(context.Background(), 5*time.Minute)
}

// TearDownTest ...
func (suite *UpdateEndpointSuite) TearDownTest() {
	if suite.ctxCancel != nil {
		suite.ctxCancel()
	}
}

// TestUpdateControlPlaneEndpoint updates a control plane endpoint to have an invalid URL,
// then asserts that the node is reported by kube-apiserver as NotReady.
// It reverts the change at the end of the test and asserts that the node is reported again as Ready.
func (suite *UpdateEndpointSuite) TestUpdateControlPlaneEndpoint() {
	if testing.Short() {
		suite.T().Skip("skipping in short mode")
	}

	nodeInternalIP := suite.RandomDiscoveredNodeInternalIP(machine.TypeControlPlane)

	node, err := suite.GetK8sNodeByInternalIP(suite.ctx, nodeInternalIP)
	suite.Require().NoError(err)

	oldURL := suite.updateEndpointURL(nodeInternalIP, "https://127.0.0.1:40443")

	nodeReady := func(status corev1.ConditionStatus) bool {
		return status == corev1.ConditionTrue
	}

	nodeNotReady := func(status corev1.ConditionStatus) bool {
		return status != corev1.ConditionTrue
	}

	defer func() {
		// revert the endpoint URL back to the original one
		suite.updateEndpointURL(nodeInternalIP, oldURL)

		// expect node status to be Ready again
		suite.Assert().NoError(suite.WaitForK8sNodeReadinessStatus(suite.ctx, node.Name, nodeReady))
	}()

	// expect node status to become NotReady
	suite.Assert().NoError(suite.WaitForK8sNodeReadinessStatus(suite.ctx, node.Name, nodeNotReady))
}

func (suite *UpdateEndpointSuite) updateEndpointURL(nodeIP string, newURL string) (oldURL string) {
	nodeCtx := client.WithNodes(suite.ctx, nodeIP)

	nodeConfig, err := suite.ReadConfigFromNode(nodeCtx)
	suite.Require().NoError(err)

	nodeConfigRaw, ok := nodeConfig.Raw().(*v1alpha1.Config)
	suite.Require().True(ok, "node config is not of type v1alpha1.Config")

	newEndpointURL, err := url.Parse(newURL)
	suite.Require().NoError(err)

	endpoint := nodeConfigRaw.ClusterConfig.ControlPlane.Endpoint
	oldURL = endpoint.URL.String()
	endpoint.URL = newEndpointURL

	bytes, err := nodeConfigRaw.Bytes()
	suite.Require().NoError(err)

	_, err = suite.Client.ApplyConfiguration(nodeCtx, &machineapi.ApplyConfigurationRequest{
		Data: bytes,
		Mode: machineapi.ApplyConfigurationRequest_NO_REBOOT,
	})

	suite.Require().NoError(err)

	return
}

func init() {
	allSuites = append(allSuites, new(UpdateEndpointSuite))
}
