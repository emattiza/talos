// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/talos-systems/go-retry/retry"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/talos-systems/talos/pkg/kubernetes"
	"github.com/talos-systems/talos/pkg/machinery/client"
	v1alpha1config "github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/talos-systems/talos/pkg/machinery/constants"
	"github.com/talos-systems/talos/pkg/machinery/resources/k8s"
	"github.com/talos-systems/talos/pkg/machinery/resources/v1alpha1"
)

const kubelet = "kubelet"

func upgradeKubelet(ctx context.Context, cluster UpgradeProvider, options UpgradeOptions) error {
	if !options.UpgradeKubelet {
		options.Log("skipped updating kubelet")

		return nil
	}

	options.Log("updating kubelet to version %q", options.ToVersion)

	for _, node := range append(append([]string(nil), options.controlPlaneNodes...), options.workerNodes...) {
		if err := upgradeKubeletOnNode(ctx, cluster, options, node); err != nil {
			return fmt.Errorf("error updating node %q: %w", node, err)
		}
	}

	return nil
}

//nolint:gocyclo,cyclop
func upgradeKubeletOnNode(ctx context.Context, cluster UpgradeProvider, options UpgradeOptions, node string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c, err := cluster.Client()
	if err != nil {
		return fmt.Errorf("error building Talos API client: %w", err)
	}

	ctx = client.WithNode(ctx, node)

	options.Log(" > %q: starting update", node)

	watchCh := make(chan safe.WrappedStateEvent[*v1alpha1.Service])

	if err = safe.StateWatch(ctx, c.COSI, resource.NewMetadata(v1alpha1.NamespaceName, v1alpha1.ServiceType, kubelet, resource.VersionUndefined), watchCh); err != nil {
		return fmt.Errorf("error watching service: %w", err)
	}

	var ev safe.WrappedStateEvent[*v1alpha1.Service]

	select {
	case ev = <-watchCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	if ev.Type() != state.Created {
		return fmt.Errorf("unexpected event type: %s", ev.Type())
	}

	initialService, err := ev.Resource()
	if err != nil {
		return fmt.Errorf("error inspecting service: %w", err)
	}

	if !initialService.TypedSpec().Running || !initialService.TypedSpec().Healthy {
		return fmt.Errorf("kubelet is not healthy")
	}

	// find out current kubelet version, as the machine config might have a missing image field,
	// look it up from the kubelet spec

	kubeletSpec, err := safe.StateGet[*k8s.KubeletSpec](ctx, c.COSI, resource.NewMetadata(k8s.NamespaceName, k8s.KubeletSpecType, kubelet, resource.VersionUndefined))
	if err != nil {
		return fmt.Errorf("error fetching kubelet spec: %w", err)
	}

	skipWait := false

	err = patchNodeConfig(ctx, cluster, node, upgradeKubeletPatcher(options, kubeletSpec))
	if err != nil {
		if errors.Is(err, errUpdateSkipped) {
			skipWait = true
		} else {
			return fmt.Errorf("error patching node config: %w", err)
		}
	}

	if options.DryRun {
		return nil
	}

	options.Log(" > %q: machine configuration patched", node)

	if !skipWait {
		options.Log(" > %q: waiting for kubelet restart", node)

		// first, wait for kubelet to go down
		for {
			select {
			case ev = <-watchCh:
			case <-ctx.Done():
				return ctx.Err()
			}

			if ev.Type() == state.Destroyed {
				break
			}
		}

		// now wait for kubelet to go up & healthy
		for {
			select {
			case ev = <-watchCh:
			case <-ctx.Done():
				return ctx.Err()
			}

			if ev.Type() == state.Created || ev.Type() == state.Updated {
				var service *v1alpha1.Service

				service, err = ev.Resource()
				if err != nil {
					return fmt.Errorf("error inspecting service: %w", err)
				}

				if service.TypedSpec().Running && service.TypedSpec().Healthy {
					break
				}
			}
		}
	}

	options.Log(" > %q: waiting for node update", node)

	if err = retry.Constant(3*time.Minute, retry.WithUnits(10*time.Second)).Retry(
		func() error {
			return checkNodeKubeletVersion(ctx, cluster, node, "v"+options.ToVersion)
		},
	); err != nil {
		return err
	}

	options.Log(" < %q: successfully updated", node)

	return nil
}

func upgradeKubeletPatcher(
	options UpgradeOptions,
	kubeletSpec *k8s.KubeletSpec,
) func(config *v1alpha1config.Config) error {
	return func(config *v1alpha1config.Config) error {
		if config.MachineConfig == nil {
			config.MachineConfig = &v1alpha1config.MachineConfig{}
		}

		if config.MachineConfig.MachineKubelet == nil {
			config.MachineConfig.MachineKubelet = &v1alpha1config.KubeletConfig{}
		}

		oldImage := kubeletSpec.TypedSpec().Image

		logUpdate := func(oldImage string) {
			parts := strings.Split(oldImage, ":")
			version := options.FromVersion

			if len(parts) > 1 {
				version = parts[1]
			}

			version = strings.TrimLeft(version, "v")

			options.Log(" > update %s: %s -> %s", kubelet, version, options.ToVersion)

			if options.DryRun {
				options.Log(" > skipped in dry-run")
			}
		}

		image := fmt.Sprintf("%s:v%s", constants.KubeletImage, options.ToVersion)

		if oldImage == image {
			return errUpdateSkipped
		}

		logUpdate(oldImage)

		if options.DryRun {
			return errUpdateSkipped
		}

		config.MachineConfig.MachineKubelet.KubeletImage = image

		return nil
	}
}

//nolint:gocyclo
func checkNodeKubeletVersion(ctx context.Context, cluster UpgradeProvider, nodeToCheck, version string) error {
	k8sClient, err := cluster.K8sHelper(ctx)
	if err != nil {
		return fmt.Errorf("error building kubernetes client: %w", err)
	}

	nodes, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		if kubernetes.IsRetryableError(err) {
			return retry.ExpectedError(err)
		}

		return err
	}

	nodeFound := false

	for _, node := range nodes.Items {
		matchingNode := false

		for _, address := range node.Status.Addresses {
			if address.Address == nodeToCheck {
				matchingNode = true

				break
			}
		}

		if !matchingNode {
			continue
		}

		nodeFound = true

		if node.Status.NodeInfo.KubeletVersion != version {
			return retry.ExpectedError(
				fmt.Errorf(
					"node version mismatch: got %q, expected %q",
					node.Status.NodeInfo.KubeletVersion,
					version,
				),
			)
		}

		ready := false

		for _, condition := range node.Status.Conditions {
			if condition.Type != v1.NodeReady {
				continue
			}

			if condition.Status == v1.ConditionTrue {
				ready = true

				break
			}
		}

		if !ready {
			return retry.ExpectedError(fmt.Errorf("node is not ready"))
		}

		break
	}

	if !nodeFound {
		return retry.ExpectedError(fmt.Errorf("node %q not found", nodeToCheck))
	}

	return nil
}
