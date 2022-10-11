// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/go-pointer"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"

	k8sadapter "github.com/talos-systems/talos/internal/app/machined/pkg/adapters/k8s"
	v1alpha1runtime "github.com/talos-systems/talos/internal/app/machined/pkg/runtime"
	machineapi "github.com/talos-systems/talos/pkg/machinery/api/machine"
	"github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1/machine"
	"github.com/talos-systems/talos/pkg/machinery/resources/config"
	"github.com/talos-systems/talos/pkg/machinery/resources/k8s"
	"github.com/talos-systems/talos/pkg/machinery/resources/network"
	"github.com/talos-systems/talos/pkg/machinery/resources/runtime"
	"github.com/talos-systems/talos/pkg/machinery/resources/time"
	"github.com/talos-systems/talos/pkg/machinery/resources/v1alpha1"
)

// MachineStatusController watches MachineStatuss, sets/resets kernel params.
type MachineStatusController struct {
	V1Alpha1Events v1alpha1runtime.Watcher

	setupOnce sync.Once

	notifyCh chan struct{}

	mu           sync.Mutex
	currentStage runtime.MachineStage
}

// Name implements controller.Controller interface.
func (ctrl *MachineStatusController) Name() string {
	return "runtime.MachineStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *MachineStatusController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: v1alpha1.NamespaceName,
			Type:      time.StatusType,
			ID:        pointer.To(time.StatusID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.StatusType,
			ID:        pointer.To(network.StatusID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: v1alpha1.NamespaceName,
			Type:      v1alpha1.ServiceType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: k8s.NamespaceName,
			Type:      k8s.StaticPodStatusType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineTypeType,
			ID:        pointer.To(config.MachineTypeID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *MachineStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.MachineStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocyclo
func (ctrl *MachineStatusController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	ctrl.setupOnce.Do(func() {
		// watcher is started once and runs for all controller runs, as if we reconnect to the event stream,
		// we might lose some state which was in the events, but it got "scrolled away" from the buffer.
		ctrl.notifyCh = make(chan struct{}, 1)
		go ctrl.watchEvents()
	})

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case <-ctrl.notifyCh:
		}

		machineTypeResource, err := safe.ReaderGet[*config.MachineType](ctx, r, config.NewMachineType().Metadata())
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting machine type: %w", err)
			}
		}

		var machineType machine.Type

		if machineTypeResource != nil {
			machineType = machineTypeResource.MachineType()
		}

		ctrl.mu.Lock()
		currentStage := ctrl.currentStage
		ctrl.mu.Unlock()

		ready := true

		var unmetConditions []runtime.UnmetCondition

		for _, check := range ctrl.getReadinessChecks(currentStage, machineType) {
			if err := check.f(ctx, r); err != nil {
				ready = false

				unmetConditions = append(unmetConditions, runtime.UnmetCondition{
					Name:   check.name,
					Reason: err.Error(),
				})
			}
		}

		if err := safe.WriterModify(ctx, r, runtime.NewMachineStatus(), func(ms *runtime.MachineStatus) error {
			ms.TypedSpec().Stage = currentStage
			ms.TypedSpec().Status.Ready = ready
			ms.TypedSpec().Status.UnmetConditions = unmetConditions

			return nil
		}); err != nil {
			return fmt.Errorf("error updating machine status: %w", err)
		}
	}
}

type readinessCheck struct {
	name string
	f    func(context.Context, controller.Runtime) error
}

func (ctrl *MachineStatusController) getReadinessChecks(stage runtime.MachineStage, machineType machine.Type) []readinessCheck {
	requiredServices := []string{
		"apid",
		"machined",
		"kubelet",
	}

	if machineType.IsControlPlane() {
		requiredServices = append(requiredServices,
			"etcd",
			"trustd",
		)
	}

	switch stage { //nolint:exhaustive
	case runtime.MachineStageBooting, runtime.MachineStageRunning:
		return []readinessCheck{
			{
				name: "time",
				f:    ctrl.timeSyncCheck,
			},
			{
				name: "network",
				f:    ctrl.networkReadyCheck,
			},
			{
				name: "services",
				f:    ctrl.servicesCheck(requiredServices),
			},
			{
				name: "staticPods",
				f:    ctrl.staticPodsCheck,
			},
		}
	default:
		return nil
	}
}

func (ctrl *MachineStatusController) timeSyncCheck(ctx context.Context, r controller.Runtime) error {
	timeSyncStatus, err := safe.ReaderGet[*time.Status](ctx, r, time.NewStatus().Metadata())
	if err != nil {
		return err
	}

	if !timeSyncStatus.TypedSpec().Synced {
		return fmt.Errorf("time is not synced")
	}

	return nil
}

func (ctrl *MachineStatusController) networkReadyCheck(ctx context.Context, r controller.Runtime) error {
	networkStatus, err := safe.ReaderGet[*network.Status](ctx, r, network.NewStatus(network.NamespaceName, network.StatusID).Metadata())
	if err != nil {
		return err
	}

	var notReady []string

	if !networkStatus.TypedSpec().AddressReady {
		notReady = append(notReady, "address")
	}

	if !networkStatus.TypedSpec().ConnectivityReady {
		notReady = append(notReady, "connectivity")
	}

	if !networkStatus.TypedSpec().EtcFilesReady {
		notReady = append(notReady, "etc-files")
	}

	if !networkStatus.TypedSpec().HostnameReady {
		notReady = append(notReady, "hostname")
	}

	if len(notReady) == 0 {
		return nil
	}

	return fmt.Errorf("waiting on: %s", strings.Join(notReady, ", "))
}

func (ctrl *MachineStatusController) servicesCheck(requiredServices []string) func(ctx context.Context, r controller.Runtime) error {
	return func(ctx context.Context, r controller.Runtime) error {
		serviceList, err := safe.ReaderList[*v1alpha1.Service](ctx, r, resource.NewMetadata(v1alpha1.NamespaceName, v1alpha1.ServiceType, "", resource.VersionUndefined))
		if err != nil {
			return err
		}

		it := safe.IteratorFromList(serviceList)

		var problems []string

		runningServices := map[string]struct{}{}

		for it.Next() {
			service := it.Value()

			if !service.TypedSpec().Running {
				problems = append(problems, fmt.Sprintf("%s not running", service.Metadata().ID()))

				continue
			}

			runningServices[service.Metadata().ID()] = struct{}{}

			if !service.TypedSpec().Unknown && !service.TypedSpec().Healthy {
				problems = append(problems, fmt.Sprintf("%s not healthy", service.Metadata().ID()))
			}
		}

		for _, svc := range requiredServices {
			if _, running := runningServices[svc]; !running {
				problems = append(problems, fmt.Sprintf("%s not running", svc))
			}
		}

		if len(problems) == 0 {
			return nil
		}

		return fmt.Errorf("%s", strings.Join(problems, ", "))
	}
}

func (ctrl *MachineStatusController) staticPodsCheck(ctx context.Context, r controller.Runtime) error {
	staticPodList, err := safe.ReaderList[*k8s.StaticPodStatus](ctx, r, resource.NewMetadata(k8s.NamespaceName, k8s.StaticPodStatusType, "", resource.VersionUndefined))
	if err != nil {
		return err
	}

	it := safe.IteratorFromList(staticPodList)

	var problems []string

	for it.Next() {
		status, err := k8sadapter.StaticPodStatus(it.Value()).Status()
		if err != nil {
			return err
		}

		ready := false

		for _, condition := range status.Conditions {
			if condition.Type == v1.PodReady {
				ready = condition.Status == v1.ConditionTrue

				break
			}
		}

		if !ready {
			problems = append(problems, fmt.Sprintf("%s not ready", it.Value().Metadata().ID()))
		}
	}

	if len(problems) == 0 {
		return nil
	}

	return fmt.Errorf("%s", strings.Join(problems, ", "))
}

//nolint:gocyclo,cyclop
func (ctrl *MachineStatusController) watchEvents() {
	// the interface of the Watch function is weird (blaming myself @smira)
	//
	// at the same time as it is events based, it's impossible to reconcile the current state
	// from the events, so what we're doing is watching the events forever as soon as the controller starts,
	// and aggregating the state into the stage variable, notifying the controller whenever the state changes.
	ctrl.V1Alpha1Events.Watch(func(eventCh <-chan v1alpha1runtime.EventInfo) { //nolint:errcheck
		var (
			oldStage        runtime.MachineStage
			currentSequence string
		)

		for ev := range eventCh {
			newStage := oldStage

			switch event := ev.Event.Payload.(type) {
			case *machineapi.SequenceEvent:
				currentSequence = event.Sequence

				switch event.Action {
				case machineapi.SequenceEvent_START:
					// mostly interested in sequence start events
					switch event.Sequence {
					case v1alpha1runtime.SequenceBoot.String(), v1alpha1runtime.SequenceInitialize.String():
						newStage = runtime.MachineStageBooting
					case v1alpha1runtime.SequenceInstall.String():
						// install sequence is run always, even if the machine is already installed, so we'll catch it by phase name
					case v1alpha1runtime.SequenceShutdown.String():
						newStage = runtime.MachineStageShuttingDown
					case v1alpha1runtime.SequenceUpgrade.String(), v1alpha1runtime.SequenceStageUpgrade.String(), v1alpha1runtime.SequenceMaintenanceUpgrade.String():
						newStage = runtime.MachineStageUpgrading
					case v1alpha1runtime.SequenceReset.String():
						newStage = runtime.MachineStageResetting
					case v1alpha1runtime.SequenceReboot.String():
						newStage = runtime.MachineStageRebooting
					}
				case machineapi.SequenceEvent_NOOP:
				case machineapi.SequenceEvent_STOP:
					if event.Sequence == v1alpha1runtime.SequenceBoot.String() && event.Error == nil {
						newStage = runtime.MachineStageRunning
					}

					// sequence finished, it doesn't matter whether if it was successful or not
					currentSequence = ""
				}
			case *machineapi.PhaseEvent:
				if event.Action == machineapi.PhaseEvent_START {
					switch {
					case currentSequence == v1alpha1runtime.SequenceInstall.String() && event.Phase == "install":
						newStage = runtime.MachineStageInstalling
					case (currentSequence == v1alpha1runtime.SequenceInstall.String() ||
						currentSequence == v1alpha1runtime.SequenceUpgrade.String() ||
						currentSequence == v1alpha1runtime.SequenceStageUpgrade.String() ||
						currentSequence == v1alpha1runtime.SequenceMaintenanceUpgrade.String()) && event.Phase == "kexec":
						newStage = runtime.MachineStageRebooting
					}
				}
			case *machineapi.TaskEvent:
				if event.Task == "runningMaintenance" {
					switch event.Action {
					case machineapi.TaskEvent_START:
						newStage = runtime.MachineStageMaintenance
					case machineapi.TaskEvent_STOP:
						newStage = runtime.MachineStageBooting
					}
				}
			}

			if oldStage != newStage {
				ctrl.mu.Lock()
				ctrl.currentStage = newStage
				ctrl.mu.Unlock()

				select {
				case ctrl.notifyCh <- struct{}{}:
				default:
				}
			}

			oldStage = newStage
		}
	}, v1alpha1runtime.WithTailEvents(-1))
}
