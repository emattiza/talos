// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//nolint:dupl
package network_test

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
	"github.com/talos-systems/go-procfs/procfs"
	"github.com/talos-systems/go-retry/retry"

	netctrl "github.com/talos-systems/talos/internal/app/machined/pkg/controllers/network"
	"github.com/talos-systems/talos/pkg/logging"
	"github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/talos-systems/talos/pkg/machinery/constants"
	"github.com/talos-systems/talos/pkg/machinery/resources/config"
	"github.com/talos-systems/talos/pkg/machinery/resources/network"
)

type TimeServerConfigSuite struct {
	suite.Suite

	state state.State

	runtime *runtime.Runtime
	wg      sync.WaitGroup

	ctx       context.Context
	ctxCancel context.CancelFunc
}

func (suite *TimeServerConfigSuite) SetupTest() {
	suite.ctx, suite.ctxCancel = context.WithTimeout(context.Background(), 3*time.Minute)

	suite.state = state.WrapCore(namespaced.NewState(inmem.Build))

	var err error

	suite.runtime, err = runtime.NewRuntime(suite.state, logging.Wrap(log.Writer()))
	suite.Require().NoError(err)
}

func (suite *TimeServerConfigSuite) startRuntime() {
	suite.wg.Add(1)

	go func() {
		defer suite.wg.Done()

		suite.Assert().NoError(suite.runtime.Run(suite.ctx))
	}()
}

func (suite *TimeServerConfigSuite) assertTimeServers(requiredIDs []string, check func(*network.TimeServerSpec) error) error {
	missingIDs := make(map[string]struct{}, len(requiredIDs))

	for _, id := range requiredIDs {
		missingIDs[id] = struct{}{}
	}

	resources, err := suite.state.List(suite.ctx, resource.NewMetadata(network.ConfigNamespaceName, network.TimeServerSpecType, "", resource.VersionUndefined))
	if err != nil {
		return err
	}

	for _, res := range resources.Items {
		_, required := missingIDs[res.Metadata().ID()]
		if !required {
			continue
		}

		delete(missingIDs, res.Metadata().ID())

		if err = check(res.(*network.TimeServerSpec)); err != nil {
			return retry.ExpectedError(err)
		}
	}

	if len(missingIDs) > 0 {
		return retry.ExpectedError(fmt.Errorf("some resources are missing: %q", missingIDs))
	}

	return nil
}

func (suite *TimeServerConfigSuite) assertNoTimeServer(id string) error {
	resources, err := suite.state.List(suite.ctx, resource.NewMetadata(network.ConfigNamespaceName, network.TimeServerSpecType, "", resource.VersionUndefined))
	if err != nil {
		return err
	}

	for _, res := range resources.Items {
		if res.Metadata().ID() == id {
			return retry.ExpectedError(fmt.Errorf("spec %q is still there", id))
		}
	}

	return nil
}

func (suite *TimeServerConfigSuite) TestDefaults() {
	suite.Require().NoError(suite.runtime.RegisterController(&netctrl.TimeServerConfigController{}))

	suite.startRuntime()

	suite.Assert().NoError(retry.Constant(3*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(
		func() error {
			return suite.assertTimeServers([]string{
				"default/timeservers",
			}, func(r *network.TimeServerSpec) error {
				suite.Assert().Equal([]string{constants.DefaultNTPServer}, r.TypedSpec().NTPServers)
				suite.Assert().Equal(network.ConfigDefault, r.TypedSpec().ConfigLayer)

				return nil
			})
		}))
}

func (suite *TimeServerConfigSuite) TestCmdline() {
	suite.Require().NoError(suite.runtime.RegisterController(&netctrl.TimeServerConfigController{
		Cmdline: procfs.NewCmdline("ip=172.20.0.2:172.21.0.1:172.20.0.1:255.255.255.0:master1:eth1::10.0.0.1:10.0.0.2:10.0.0.1"),
	}))

	suite.startRuntime()

	suite.Assert().NoError(retry.Constant(3*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(
		func() error {
			return suite.assertTimeServers([]string{
				"cmdline/timeservers",
			}, func(r *network.TimeServerSpec) error {
				suite.Assert().Equal([]string{"10.0.0.1"}, r.TypedSpec().NTPServers)

				return nil
			})
		}))
}

func (suite *TimeServerConfigSuite) TestMachineConfiguration() {
	suite.Require().NoError(suite.runtime.RegisterController(&netctrl.TimeServerConfigController{}))

	suite.startRuntime()

	u, err := url.Parse("https://foo:6443")
	suite.Require().NoError(err)

	cfg := config.NewMachineConfig(&v1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &v1alpha1.MachineConfig{
			MachineTime: &v1alpha1.TimeConfig{
				TimeServers: []string{"za.pool.ntp.org", "pool.ntp.org"},
			},
		},
		ClusterConfig: &v1alpha1.ClusterConfig{
			ControlPlane: &v1alpha1.ControlPlaneConfig{
				Endpoint: &v1alpha1.Endpoint{
					URL: u,
				},
			},
		},
	})

	suite.Require().NoError(suite.state.Create(suite.ctx, cfg))

	suite.Assert().NoError(retry.Constant(3*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(
		func() error {
			return suite.assertTimeServers([]string{
				"configuration/timeservers",
			}, func(r *network.TimeServerSpec) error {
				suite.Assert().Equal([]string{"za.pool.ntp.org", "pool.ntp.org"}, r.TypedSpec().NTPServers)

				return nil
			})
		}))

	_, err = suite.state.UpdateWithConflicts(suite.ctx, cfg.Metadata(), func(r resource.Resource) error {
		r.(*config.MachineConfig).Config().(*v1alpha1.Config).MachineConfig.MachineTime = nil

		return nil
	})
	suite.Require().NoError(err)

	suite.Assert().NoError(retry.Constant(3*time.Second, retry.WithUnits(100*time.Millisecond)).Retry(
		func() error {
			return suite.assertNoTimeServer("configuration/timeservers")
		}))
}

func (suite *TimeServerConfigSuite) TearDownTest() {
	suite.T().Log("tear down")

	suite.ctxCancel()

	suite.wg.Wait()

	// trigger updates in resources to stop watch loops
	err := suite.state.Create(context.Background(), config.NewMachineConfig(&v1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &v1alpha1.MachineConfig{},
	}))
	if state.IsConflictError(err) {
		err = suite.state.Destroy(context.Background(), config.NewMachineConfig(nil).Metadata())
	}

	suite.Require().NoError(err)
}

func TestTimeServerConfigSuite(t *testing.T) {
	suite.Run(t, new(TimeServerConfigSuite))
}
