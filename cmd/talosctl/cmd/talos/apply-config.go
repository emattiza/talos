// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/talos-systems/talos/cmd/talosctl/pkg/talos/helpers"
	"github.com/talos-systems/talos/internal/pkg/tui/installer"
	machineapi "github.com/talos-systems/talos/pkg/machinery/api/machine"
	"github.com/talos-systems/talos/pkg/machinery/client"
	"github.com/talos-systems/talos/pkg/machinery/config/configpatcher"
	"github.com/talos-systems/talos/pkg/machinery/constants"
)

var applyConfigCmdFlags struct {
	helpers.Mode
	certFingerprints []string
	patches          []string
	filename         string
	insecure         bool
	dryRun           bool
	configTryTimeout time.Duration
}

// applyConfigCmd represents the applyConfiguration command.
var applyConfigCmd = &cobra.Command{
	Use:     "apply-config",
	Aliases: []string{"apply"},
	Short:   "Apply a new configuration to a node",
	Long:    ``,
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			cfgBytes []byte
			e        error
		)

		if len(args) > 0 {
			if args[0] != "config" && !strings.EqualFold(args[0], "machineconfig") {
				cmd.Help() //nolint:errcheck

				return fmt.Errorf("unknown positional argument %s", args[0])
			} else if cmd.CalledAs() == "apply-config" {
				cmd.Help() //nolint:errcheck

				return fmt.Errorf("expected no positional arguments")
			}
		}

		if applyConfigCmdFlags.filename != "" {
			cfgBytes, e = os.ReadFile(applyConfigCmdFlags.filename)
			if e != nil {
				return fmt.Errorf("failed to read configuration from %q: %w", applyConfigCmdFlags.filename, e)
			}

			if len(cfgBytes) < 1 {
				return fmt.Errorf("no configuration data read")
			}

			if len(applyConfigCmdFlags.patches) != 0 {
				var (
					cfg     configpatcher.Input
					patches []configpatcher.Patch
				)

				patches, e = configpatcher.LoadPatches(applyConfigCmdFlags.patches)
				if e != nil {
					return e
				}

				cfg, e = configpatcher.Apply(configpatcher.WithBytes(cfgBytes), patches)
				if e != nil {
					return e
				}

				cfgBytes, e = cfg.Bytes()
				if e != nil {
					return e
				}
			}
		} else if !applyConfigCmdFlags.Interactive {
			return fmt.Errorf("no filename supplied for configuration")
		}

		withClient := func(f func(context.Context, *client.Client) error) error {
			if applyConfigCmdFlags.insecure {
				return WithClientMaintenance(applyConfigCmdFlags.certFingerprints, f)
			}

			return WithClient(f)
		}

		return withClient(func(ctx context.Context, c *client.Client) error {
			if applyConfigCmdFlags.Interactive {
				install := installer.NewInstaller()
				node := GlobalArgs.Nodes[0]

				if len(GlobalArgs.Endpoints) > 0 {
					return WithClientNoNodes(func(bootstrapCtx context.Context, bootstrapClient *client.Client) error {
						opts := []installer.Option{}
						opts = append(opts, installer.WithBootstrapNode(bootstrapCtx, bootstrapClient, GlobalArgs.Endpoints[0]), installer.WithDryRun(applyConfigCmdFlags.dryRun))

						conn, err := installer.NewConnection(
							ctx,
							c,
							node,
							opts...,
						)
						if err != nil {
							return err
						}

						return install.Run(conn)
					})
				}

				conn, err := installer.NewConnection(
					ctx,
					c,
					node,
					installer.WithDryRun(applyConfigCmdFlags.dryRun),
				)
				if err != nil {
					return err
				}

				return install.Run(conn)
			}

			resp, err := c.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
				Data:           cfgBytes,
				Mode:           applyConfigCmdFlags.Mode.Mode,
				OnReboot:       applyConfigCmdFlags.OnReboot,
				Immediate:      applyConfigCmdFlags.Immediate,
				DryRun:         applyConfigCmdFlags.dryRun,
				TryModeTimeout: durationpb.New(applyConfigCmdFlags.configTryTimeout),
			})
			if err != nil {
				return fmt.Errorf("error applying new configuration: %s", err)
			}

			helpers.PrintApplyResults(resp)

			return nil
		})
	},
}

func init() {
	applyConfigCmd.Flags().StringVarP(&applyConfigCmdFlags.filename, "file", "f", "", "the filename of the updated configuration")
	applyConfigCmd.Flags().BoolVarP(&applyConfigCmdFlags.insecure, "insecure", "i", false, "apply the config using the insecure (encrypted with no auth) maintenance service")
	applyConfigCmd.Flags().BoolVar(&applyConfigCmdFlags.dryRun, "dry-run", false, "check how the config change will be applied in dry-run mode")
	applyConfigCmd.Flags().StringSliceVar(&applyConfigCmdFlags.certFingerprints, "cert-fingerprint", nil, "list of server certificate fingeprints to accept (defaults to no check)")
	applyConfigCmd.Flags().StringSliceVarP(&applyConfigCmdFlags.patches, "config-patch", "p", nil, "the list of config patches to apply to the local config file before sending it to the node")
	applyConfigCmd.Flags().DurationVar(&applyConfigCmdFlags.configTryTimeout, "timeout", constants.ConfigTryTimeout, "the config will be rolled back after specified timeout (if try mode is selected)")
	helpers.AddModeFlags(&applyConfigCmdFlags.Mode, applyConfigCmd)
	addCommand(applyConfigCmd)
}
