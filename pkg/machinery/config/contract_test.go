// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/talos-systems/talos/pkg/machinery/config"
)

func TestContractGreater(t *testing.T) {
	assert.True(t, config.TalosVersion0_9.Greater(config.TalosVersion0_8))
	assert.True(t, config.TalosVersionCurrent.Greater(config.TalosVersion0_8))
	assert.True(t, config.TalosVersionCurrent.Greater(config.TalosVersion0_9))

	assert.False(t, config.TalosVersion0_8.Greater(config.TalosVersion0_9))
	assert.False(t, config.TalosVersion0_8.Greater(config.TalosVersion0_8))
	assert.False(t, config.TalosVersionCurrent.Greater(config.TalosVersionCurrent))
}

func TestContractParseVersion(t *testing.T) {
	t.Parallel()

	for v, expected := range map[string]*config.VersionContract{
		"v0.8":           config.TalosVersion0_8,
		"v0.8.":          config.TalosVersion0_8,
		"v0.8.1":         config.TalosVersion0_8,
		"v0.88":          {0, 88},
		"v0.8.3-alpha.4": config.TalosVersion0_8,
	} {
		v, expected := v, expected
		t.Run(v, func(t *testing.T) {
			t.Parallel()

			actual, err := config.ParseContractFromVersion(v)
			assert.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}
}

func TestContractCurrent(t *testing.T) {
	contract := config.TalosVersionCurrent

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.True(t, contract.SupportsRBACFeature())
	assert.True(t, contract.SupportsDynamicCertSANs())
	assert.True(t, contract.SupportsECDSASHA256())
	assert.True(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_14(t *testing.T) {
	contract := config.TalosVersion0_14

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.True(t, contract.SupportsRBACFeature())
	assert.True(t, contract.SupportsDynamicCertSANs())
	assert.True(t, contract.SupportsECDSASHA256())
	assert.True(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_13(t *testing.T) {
	contract := config.TalosVersion0_13

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.True(t, contract.SupportsRBACFeature())
	assert.True(t, contract.SupportsDynamicCertSANs())
	assert.True(t, contract.SupportsECDSASHA256())
	assert.False(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_12(t *testing.T) {
	contract := config.TalosVersion0_12

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.True(t, contract.SupportsRBACFeature())
	assert.False(t, contract.SupportsDynamicCertSANs())
	assert.False(t, contract.SupportsECDSASHA256())
	assert.False(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_11(t *testing.T) {
	contract := config.TalosVersion0_11

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.True(t, contract.SupportsRBACFeature())
	assert.False(t, contract.SupportsDynamicCertSANs())
	assert.False(t, contract.SupportsECDSASHA256())
	assert.False(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_10(t *testing.T) {
	contract := config.TalosVersion0_10

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.False(t, contract.SupportsRBACFeature())
	assert.False(t, contract.SupportsDynamicCertSANs())
	assert.False(t, contract.SupportsECDSASHA256())
	assert.False(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_9(t *testing.T) {
	contract := config.TalosVersion0_9

	assert.True(t, contract.SupportsAggregatorCA())
	assert.True(t, contract.SupportsECDSAKeys())
	assert.True(t, contract.SupportsServiceAccount())
	assert.False(t, contract.SupportsRBACFeature())
	assert.False(t, contract.SupportsDynamicCertSANs())
	assert.False(t, contract.SupportsECDSASHA256())
	assert.False(t, contract.ClusterDiscoveryEnabled())
}

func TestContract0_8(t *testing.T) {
	contract := config.TalosVersion0_8

	assert.False(t, contract.SupportsAggregatorCA())
	assert.False(t, contract.SupportsECDSAKeys())
	assert.False(t, contract.SupportsServiceAccount())
	assert.False(t, contract.SupportsRBACFeature())
	assert.False(t, contract.SupportsDynamicCertSANs())
	assert.False(t, contract.SupportsECDSASHA256())
	assert.False(t, contract.ClusterDiscoveryEnabled())
}
