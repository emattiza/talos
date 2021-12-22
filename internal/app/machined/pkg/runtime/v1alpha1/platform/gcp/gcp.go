// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package gcp

import (
	"context"
	"encoding/json"
	"log"
	"net"

	"github.com/talos-systems/go-procfs/procfs"

	"github.com/talos-systems/talos/internal/app/machined/pkg/runtime"
	"github.com/talos-systems/talos/internal/app/machined/pkg/runtime/v1alpha1/platform/errors"
	"github.com/talos-systems/talos/pkg/download"
)

// Ref: https://cloud.google.com/compute/docs/storing-retrieving-metadata
// ex, curl -H "Metadata-Flavor: Google" 'http://169.254.169.254/computeMetadata/v1/instance/network-interfaces/?recursive=true'
const (
	// GCUserDataEndpoint is the local metadata endpoint inside of DO.
	GCUserDataEndpoint = "http://metadata.google.internal/computeMetadata/v1/instance/attributes/user-data"
	// GCExternalIPEndpoint displays all external addresses associated with the instance.
	GCExternalIPEndpoint = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/?recursive=true"
)

// GCP is the concrete type that implements the platform.Platform interface.
type GCP struct{}

// Name implements the platform.Platform interface.
func (g *GCP) Name() string {
	return "gcp"
}

// Configuration implements the platform.Platform interface.
func (g *GCP) Configuration(ctx context.Context) ([]byte, error) {
	log.Printf("fetching machine config from: %q", GCUserDataEndpoint)

	return download.Download(ctx, GCUserDataEndpoint,
		download.WithHeaders(map[string]string{"Metadata-Flavor": "Google"}),
		download.WithErrorOnNotFound(errors.ErrNoConfigSource),
		download.WithErrorOnEmptyResponse(errors.ErrNoConfigSource))
}

// Hostname implements the platform.Platform interface.
func (g *GCP) Hostname(context.Context) (hostname []byte, err error) {
	return nil, nil
}

// Mode implements the platform.Platform interface.
func (g *GCP) Mode() runtime.Mode {
	return runtime.ModeCloud
}

// ExternalIPs implements the runtime.Platform interface.
func (g *GCP) ExternalIPs(ctx context.Context) (addrs []net.IP, err error) {
	log.Printf("fetching externalIP from: %q", GCExternalIPEndpoint)

	metadataNetworkConfig, err := download.Download(ctx, GCExternalIPEndpoint,
		download.WithHeaders(map[string]string{"Metadata-Flavor": "Google"}),
		download.WithErrorOnNotFound(errors.ErrNoExternalIPs),
		download.WithErrorOnEmptyResponse(errors.ErrNoExternalIPs))
	if err != nil {
		return nil, err
	}

	type metadata []struct {
		AccessConfigs []struct {
			ExternalIP string `json:"externalIp"`
		} `json:"accessConfigs"`
	}

	m := metadata{}
	if err = json.Unmarshal(metadataNetworkConfig, &m); err != nil {
		return nil, err
	}

	for _, networkInterface := range m {
		for _, accessConfig := range networkInterface.AccessConfigs {
			if addr := net.ParseIP(accessConfig.ExternalIP); addr != nil {
				addrs = append(addrs, addr)
			}
		}
	}

	return addrs, nil
}

// KernelArgs implements the runtime.Platform interface.
func (g *GCP) KernelArgs() procfs.Parameters {
	return []*procfs.Parameter{
		procfs.NewParameter("console").Append("ttyS0"),
	}
}
