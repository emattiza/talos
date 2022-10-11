// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//nolint:dupl
package config_test

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/stretchr/testify/suite"
	"github.com/talos-systems/go-retry/retry"

	configctrl "github.com/talos-systems/talos/internal/app/machined/pkg/controllers/config"
	"github.com/talos-systems/talos/pkg/logging"
	"github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/talos-systems/talos/pkg/machinery/resources/config"
	"github.com/talos-systems/talos/pkg/machinery/resources/k8s"
	"github.com/talos-systems/talos/pkg/machinery/resources/network"
)

type K8sAddressFilterSuite struct {
	suite.Suite

	state state.State

	runtime *runtime.Runtime
	wg      sync.WaitGroup

	//nolint:containedctx
	ctx       context.Context
	ctxCancel context.CancelFunc
}

func (suite *K8sAddressFilterSuite) SetupTest() {
	suite.ctx, suite.ctxCancel = context.WithTimeout(context.Background(), 3*time.Minute)

	suite.state = state.WrapCore(namespaced.NewState(inmem.Build))

	var err error

	suite.runtime, err = runtime.NewRuntime(suite.state, logging.Wrap(log.Writer()))
	suite.Require().NoError(err)

	suite.Require().NoError(suite.runtime.RegisterController(&configctrl.K8sAddressFilterController{}))

	suite.startRuntime()
}

func (suite *K8sAddressFilterSuite) startRuntime() {
	suite.wg.Add(1)

	go func() {
		defer suite.wg.Done()

		suite.Assert().NoError(suite.runtime.Run(suite.ctx))
	}()
}

func (suite *K8sAddressFilterSuite) assertResource(
	md resource.Metadata,
	check func(res resource.Resource) error,
) func() error {
	return func() error {
		r, err := suite.state.Get(suite.ctx, md)
		if err != nil {
			if state.IsNotFoundError(err) {
				return retry.ExpectedError(err)
			}

			return err
		}

		return check(r)
	}
}

func (suite *K8sAddressFilterSuite) TestReconcile() {
	u, err := url.Parse("https://foo:6443")
	suite.Require().NoError(err)

	cfg := config.NewMachineConfig(
		&v1alpha1.Config{
			ConfigVersion: "v1alpha1",
			MachineConfig: &v1alpha1.MachineConfig{},
			ClusterConfig: &v1alpha1.ClusterConfig{
				ControlPlane: &v1alpha1.ControlPlaneConfig{
					Endpoint: &v1alpha1.Endpoint{
						URL: u,
					},
				},
				ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
					ServiceSubnet: []string{
						"10.200.0.0/22",
						"fd40:10:200::/112",
					},
					PodSubnet: []string{
						"10.32.0.0/12",
						"fd00:10:32::/102",
					},
				},
			},
		},
	)
	suite.Require().NoError(suite.state.Create(suite.ctx, cfg))

	suite.Assert().NoError(
		retry.Constant(3*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(
			suite.assertResource(
				resource.NewMetadata(
					network.NamespaceName,
					network.NodeAddressFilterType,
					k8s.NodeAddressFilterOnlyK8s,
					resource.VersionUndefined,
				),
				func(res resource.Resource) error {
					spec := res.(*network.NodeAddressFilter).TypedSpec()

					suite.Assert().Equal(
						"[10.32.0.0/12 fd00:10:32::/102 10.200.0.0/22 fd40:10:200::/112]",
						fmt.Sprintf("%s", spec.IncludeSubnets),
					)
					suite.Assert().Empty(spec.ExcludeSubnets)

					return nil
				},
			),
		),
	)

	suite.Assert().NoError(
		retry.Constant(3*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(
			suite.assertResource(
				resource.NewMetadata(
					network.NamespaceName,
					network.NodeAddressFilterType,
					k8s.NodeAddressFilterNoK8s,
					resource.VersionUndefined,
				),
				func(res resource.Resource) error {
					spec := res.(*network.NodeAddressFilter).TypedSpec()

					suite.Assert().Empty(spec.IncludeSubnets)
					suite.Assert().Equal(
						"[10.32.0.0/12 fd00:10:32::/102 10.200.0.0/22 fd40:10:200::/112]",
						fmt.Sprintf("%s", spec.ExcludeSubnets),
					)

					return nil
				},
			),
		),
	)
}

func (suite *K8sAddressFilterSuite) TearDownTest() {
	suite.T().Log("tear down")

	suite.ctxCancel()

	suite.wg.Wait()
}

func TestK8sAddressFilterSuite(t *testing.T) {
	suite.Run(t, new(K8sAddressFilterSuite))
}
