// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration_api

package api

import (
	"context"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"golang.org/x/sync/errgroup"

	"github.com/talos-systems/talos/internal/integration/base"
	"github.com/talos-systems/talos/pkg/machinery/client"
	"github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1/machine"
	"github.com/talos-systems/talos/pkg/machinery/resources/v1alpha1"
)

// ResourcesSuite ...
type ResourcesSuite struct {
	base.APISuite

	ctx       context.Context //nolint:containedctx
	ctxCancel context.CancelFunc
}

// SuiteName ...
func (suite *ResourcesSuite) SuiteName() string {
	return "api.ResourcesSuite"
}

// SetupTest ...
func (suite *ResourcesSuite) SetupTest() {
	suite.ctx, suite.ctxCancel = context.WithTimeout(context.Background(), time.Minute)
}

// TearDownTest ...
func (suite *ResourcesSuite) TearDownTest() {
	if suite.ctxCancel != nil {
		suite.ctxCancel()
	}
}

// TestListResources tries to fetch every resource in the system.
func (suite *ResourcesSuite) TestListResources() {
	node := suite.RandomDiscoveredNodeInternalIP(machine.TypeControlPlane)
	ctx := client.WithNode(suite.ctx, node)

	var namespaces []string

	nsList, err := safe.StateList[*meta.Namespace](ctx, suite.Client.COSI, resource.NewMetadata(meta.NamespaceName, meta.NamespaceType, "", resource.VersionUndefined))
	suite.Require().NoError(err)

	nsIt := safe.IteratorFromList(nsList)

	for nsIt.Next() {
		namespaces = append(namespaces, nsIt.Value().Metadata().ID())
	}

	var resourceTypes []string

	rdList, err := safe.StateList[*meta.ResourceDefinition](ctx, suite.Client.COSI, resource.NewMetadata(meta.NamespaceName, meta.ResourceDefinitionType, "", resource.VersionUndefined))
	suite.Require().NoError(err)

	rdIt := safe.IteratorFromList(rdList)

	for rdIt.Next() {
		resourceTypes = append(resourceTypes, rdIt.Value().TypedSpec().Type)
	}

	eg, egCtx := errgroup.WithContext(ctx)

	for _, resourceType := range resourceTypes {
		resourceType := resourceType

		eg.Go(func() error {
			for _, namespace := range namespaces {
				_, err := suite.Client.COSI.List(egCtx, resource.NewMetadata(namespace, resourceType, "", resource.VersionUndefined))
				if err != nil {
					return fmt.Errorf("failed to list resources of type %q in namespace %q: %w", resourceType, namespace, err)
				}
			}

			return nil
		})
	}

	suite.Assert().NoError(eg.Wait())
}

// TestForbiddenOperations verifies that write operations are forbidden.
func (suite *ResourcesSuite) TestForbiddenOperations() {
	node := suite.RandomDiscoveredNodeInternalIP()
	ctx := client.WithNode(suite.ctx, node)

	err := suite.Client.COSI.Create(ctx, v1alpha1.NewService("foo"))
	suite.Require().Error(err)
	suite.Assert().True(state.IsConflictError(err)) // this is how COSI wraps the error

	err = suite.Client.COSI.Destroy(ctx, v1alpha1.NewService("kubelet").Metadata())
	suite.Require().Error(err)
	suite.Assert().True(state.IsConflictError(err)) // this is how COSI wraps the error

	err = suite.Client.COSI.Update(ctx, v1alpha1.NewService("kubelet"))
	suite.Require().Error(err)
	suite.Assert().True(state.IsConflictError(err)) // this is how COSI wraps the error
}

func init() {
	allSuites = append(allSuites, new(ResourcesSuite))
}
