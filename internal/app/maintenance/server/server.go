// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package server

import (
	"context"
	"fmt"
	"log"
	"strings"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/protobuf/server"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/talos-systems/talos/internal/app/machined/pkg/runtime"
	"github.com/talos-systems/talos/internal/app/resources"
	storaged "github.com/talos-systems/talos/internal/app/storaged"
	"github.com/talos-systems/talos/internal/pkg/configuration"
	"github.com/talos-systems/talos/pkg/machinery/api/machine"
	"github.com/talos-systems/talos/pkg/machinery/api/resource"
	"github.com/talos-systems/talos/pkg/machinery/api/storage"
	"github.com/talos-systems/talos/pkg/machinery/config/configloader"
	v1alpha1machine "github.com/talos-systems/talos/pkg/machinery/config/types/v1alpha1/machine"
	"github.com/talos-systems/talos/pkg/version"
)

// Server implements machine.MachineService, network.NetworkService, and storage.StorageService.
type Server struct {
	machine.UnimplementedMachineServiceServer

	controller runtime.Controller
	logger     *log.Logger
	cfgCh      chan<- []byte
	server     *grpc.Server
}

// New initializes and returns a `Server`.
func New(c runtime.Controller, logger *log.Logger, cfgCh chan<- []byte) *Server {
	return &Server{
		controller: c,
		logger:     logger,
		cfgCh:      cfgCh,
	}
}

// Register implements the factory.Registrator interface.
func (s *Server) Register(obj *grpc.Server) {
	s.server = obj

	// wrap resources with access filter
	resourceState := s.controller.Runtime().State().V1Alpha2().Resources()
	resourceState = state.WrapCore(state.Filter(resourceState, resources.AccessPolicy(resourceState)))

	storage.RegisterStorageServiceServer(obj, &storaged.Server{})
	machine.RegisterMachineServiceServer(obj, s)
	resource.RegisterResourceServiceServer(obj, &resources.Server{Resources: resourceState}) //nolint:staticcheck
	cosiv1alpha1.RegisterStateServer(obj, server.NewState(resourceState))
}

// ApplyConfiguration implements machine.MachineService.
func (s *Server) ApplyConfiguration(ctx context.Context, in *machine.ApplyConfigurationRequest) (*machine.ApplyConfigurationResponse, error) {
	//nolint:exhaustive
	switch in.Mode {
	case machine.ApplyConfigurationRequest_TRY:
		fallthrough
	case machine.ApplyConfigurationRequest_REBOOT:
		fallthrough
	case machine.ApplyConfigurationRequest_AUTO:
	default:
		return nil, status.Errorf(codes.Unimplemented, "apply configuration --mode='%s' is not supported in maintenance mode",
			strings.ReplaceAll(strings.ToLower(in.Mode.String()), "_", "-"))
	}

	cfgProvider, err := configloader.NewFromBytes(in.GetData())
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	warnings, err := cfgProvider.Validate(s.controller.Runtime().State().Platform().Mode())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "configuration validation failed: %s", err)
	}

	reply := &machine.ApplyConfigurationResponse{
		Messages: []*machine.ApplyConfiguration{
			{
				Warnings: warnings,
			},
		},
	}

	if in.DryRun {
		reply.Messages[0].ModeDetails = `Dry run summary:
Node is running in maintenance mode and does not have a config yet.`

		return reply, nil
	}

	s.cfgCh <- in.GetData()

	return reply, nil
}

// GenerateConfiguration implements the machine.MachineServer interface.
func (s *Server) GenerateConfiguration(ctx context.Context, in *machine.GenerateConfigurationRequest) (*machine.GenerateConfigurationResponse, error) {
	if in.MachineConfig == nil {
		return nil, fmt.Errorf("invalid generate request")
	}

	machineType := v1alpha1machine.Type(in.MachineConfig.Type)

	if machineType == v1alpha1machine.TypeWorker {
		return nil, fmt.Errorf("join config can't be generated in the maintenance mode")
	}

	return configuration.Generate(ctx, in)
}

// GenerateClientConfiguration implements the machine.MachineServer interface.
func (s *Server) GenerateClientConfiguration(ctx context.Context, in *machine.GenerateClientConfigurationRequest) (*machine.GenerateClientConfigurationResponse, error) {
	return nil, status.Error(codes.Unimplemented, "client configuration (talosconfig) can't be generated in the maintenance mode")
}

// Version implements the machine.MachineServer interface.
func (s *Server) Version(ctx context.Context, in *emptypb.Empty) (*machine.VersionResponse, error) {
	if err := assertPeerSideroLink(ctx); err != nil {
		return nil, err
	}

	var platform *machine.PlatformInfo

	if s.controller.Runtime().State().Platform() != nil {
		platform = &machine.PlatformInfo{
			Name: s.controller.Runtime().State().Platform().Name(),
			Mode: s.controller.Runtime().State().Platform().Mode().String(),
		}
	}

	return &machine.VersionResponse{
		Messages: []*machine.Version{
			{
				Version:  version.NewVersion(),
				Platform: platform,
			},
		},
	}, nil
}

// Upgrade initiates an upgrade.
//
//nolint:gocyclo,cyclop
func (s *Server) Upgrade(ctx context.Context, in *machine.UpgradeRequest) (reply *machine.UpgradeResponse, err error) {
	if err = assertPeerSideroLink(ctx); err != nil {
		return nil, err
	}

	if s.controller.Runtime().State().Machine().Disk() == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Talos is not installed")
	}

	actorID := uuid.New().String()

	mode := s.controller.Runtime().State().Platform().Mode()

	if !mode.Supports(runtime.Upgrade) {
		return nil, status.Errorf(codes.FailedPrecondition, "method is not supported in %s mode", mode.String())
	}

	// none of the options are supported in maintenance mode
	if in.GetPreserve() || in.GetStage() || in.GetForce() {
		return nil, status.Errorf(codes.Unimplemented, "upgrade --preserve, --stage, and --force are not supported in maintenance mode")
	}

	log.Printf("upgrade request received: %q", in.GetImage())

	runCtx := context.WithValue(context.Background(), runtime.ActorIDCtxKey{}, actorID)

	go func() {
		if err := s.controller.Run(runCtx, runtime.SequenceMaintenanceUpgrade, in); err != nil {
			if !runtime.IsRebootError(err) {
				log.Println("upgrade failed:", err)
			}
		}
	}()

	reply = &machine.UpgradeResponse{
		Messages: []*machine.Upgrade{
			{
				Ack:     "Upgrade request received",
				ActorId: actorID,
			},
		},
	}

	return reply, nil
}
