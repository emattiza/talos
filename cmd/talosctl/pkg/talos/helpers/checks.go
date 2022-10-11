// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package helpers

import (
	"context"
	"fmt"
	"os"
	"strings"

	hashiversion "github.com/hashicorp/go-version"
	"google.golang.org/grpc/metadata"

	"github.com/talos-systems/talos/pkg/machinery/api/common"
	"github.com/talos-systems/talos/pkg/machinery/client"
	"github.com/talos-systems/talos/pkg/version"
)

// FailIfMultiNodes checks if ctx contains multi-node request metadata.
func FailIfMultiNodes(ctx context.Context, command string) error {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return nil
	}

	if len(md.Get("nodes")) <= 1 {
		return nil
	}

	return fmt.Errorf("command %q is not supported with multiple nodes", command)
}

// CheckErrors goes through the returned message list and checks if any messages have errors set.
func CheckErrors[T interface{ GetMetadata() *common.Metadata }](messages ...T) error {
	var err error

	for _, msg := range messages {
		md := msg.GetMetadata()
		if md.Error != "" {
			err = AppendErrors(err, fmt.Errorf(md.Error))
		}
	}

	return err
}

// ClientVersionCheck verifies that client is not outdated vs. Talos version.
func ClientVersionCheck(ctx context.Context, c *client.Client) error {
	// ignore the error, as we are only interested in the nodes which respond
	serverVersions, _ := c.Version(ctx) //nolint:errcheck

	clientVersion, err := hashiversion.NewVersion(version.NewVersion().Tag)
	if err != nil {
		return fmt.Errorf("error parsing client version: %w", err)
	}

	var warnings []string

	for _, msg := range serverVersions.GetMessages() {
		node := msg.GetMetadata().GetHostname()

		serverVersion, err := hashiversion.NewVersion(msg.GetVersion().Tag)
		if err != nil {
			return fmt.Errorf("%s: error parsing server version: %w", node, err)
		}

		if serverVersion.Compare(clientVersion) < 0 {
			warnings = append(warnings, fmt.Sprintf("%s: server version %s is older than client version %s", node, serverVersion, clientVersion))
		}
	}

	if warnings != nil {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", strings.Join(warnings, ", "))
	}

	return nil
}
