// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package kubespan

import (
	"fmt"
	"net"

	"github.com/mdlayher/netx/eui64"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"inet.af/netaddr"

	"github.com/talos-systems/talos/pkg/machinery/resources/kubespan"
	"github.com/talos-systems/talos/pkg/machinery/resources/network"
)

// IdentitySpec adapter provides identity generation.
//
//nolint:revive,golint
func IdentitySpec(r *kubespan.IdentitySpec) identity {
	return identity{
		IdentitySpec: r,
	}
}

type identity struct {
	*kubespan.IdentitySpec
}

// GenerateKey generates new Wireguard key.
func (a identity) GenerateKey() error {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return err
	}

	a.IdentitySpec.PrivateKey = key.String()
	a.IdentitySpec.PublicKey = key.PublicKey().String()

	return nil
}

// UpdateAddress re-calculates node address based on input data.
func (a identity) UpdateAddress(clusterID string, mac net.HardwareAddr) error {
	a.IdentitySpec.Subnet = network.ULAPrefix(clusterID, network.ULAKubeSpan)

	var err error

	a.IdentitySpec.Address, err = wgEUI64(a.IdentitySpec.Subnet, mac)

	return err
}

func wgEUI64(prefix netaddr.IPPrefix, mac net.HardwareAddr) (out netaddr.IPPrefix, err error) {
	if prefix.IsZero() {
		return out, fmt.Errorf("cannot calculate IP from zero prefix")
	}

	stdIP, err := eui64.ParseMAC(prefix.IPNet().IP, mac)
	if err != nil {
		return out, fmt.Errorf("failed to parse MAC into EUI-64 address: %w", err)
	}

	ip, ok := netaddr.FromStdIP(stdIP)
	if !ok {
		return out, fmt.Errorf("failed to parse intermediate standard IP %q: %w", stdIP.String(), err)
	}

	return netaddr.IPPrefixFrom(ip, ip.BitLen()), nil
}
