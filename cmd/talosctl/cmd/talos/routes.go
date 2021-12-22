// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/talos-systems/talos/pkg/machinery/client"
)

// routesCmd represents the net routes command.
var routesCmd = &cobra.Command{
	Use:     "routes",
	Aliases: []string{"route"},
	Short:   "List network routes",
	Long:    ``,
	Args:    cobra.NoArgs,
	Hidden:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return WithClient(func(ctx context.Context, c *client.Client) error {
			return fmt.Errorf("`talosctl routes` is deprecated, please use `talosctl get routes` instead")
		})
	},
}

func init() {
	addCommand(routesCmd)
}
