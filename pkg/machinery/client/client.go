// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package client provides Talos API client.
package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/protobuf/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"

	clusterapi "github.com/talos-systems/talos/pkg/machinery/api/cluster"
	"github.com/talos-systems/talos/pkg/machinery/api/common"
	inspectapi "github.com/talos-systems/talos/pkg/machinery/api/inspect"
	machineapi "github.com/talos-systems/talos/pkg/machinery/api/machine"
	resourceapi "github.com/talos-systems/talos/pkg/machinery/api/resource"
	storageapi "github.com/talos-systems/talos/pkg/machinery/api/storage"
	timeapi "github.com/talos-systems/talos/pkg/machinery/api/time"
	clientconfig "github.com/talos-systems/talos/pkg/machinery/client/config"
)

// Client implements the proto.MachineServiceClient interface. It serves as the
// concrete type with the required methods.
type Client struct {
	options *Options
	conn    *grpcConnectionWrapper

	MachineClient machineapi.MachineServiceClient
	TimeClient    timeapi.TimeServiceClient
	ClusterClient clusterapi.ClusterServiceClient
	StorageClient storageapi.StorageServiceClient
	InspectClient inspectapi.InspectServiceClient

	// Deprecated: use COSI client.
	Resources      *ResourcesClient
	ResourceClient resourceapi.ResourceServiceClient //nolint:staticcheck

	COSI state.State

	Inspect *InspectClient
}

func (c *Client) resolveConfigContext() error {
	var ok bool

	if c.options.unixSocketPath != "" {
		return nil
	}

	if c.options.configContext != nil {
		return nil
	}

	if c.options.config == nil {
		if err := WithDefaultConfig()(c.options); err != nil {
			return fmt.Errorf("failed to load default config: %w", err)
		}
	}

	if c.options.contextOverrideSet {
		c.options.configContext, ok = c.options.config.Contexts[c.options.contextOverride]
		if !ok {
			return fmt.Errorf("context %q not found in config", c.options.contextOverride)
		}

		return nil
	}

	c.options.configContext, ok = c.options.config.Contexts[c.options.config.Context]
	if !ok {
		if c.options.config.Context == "" && len(c.options.config.Contexts) == 0 {
			return fmt.Errorf("talos config file is empty")
		}

		return fmt.Errorf("default context %q not found in config", c.options.config.Context)
	}

	return nil
}

// GetConfigContext returns resolved config context.
func (c *Client) GetConfigContext() *clientconfig.Context {
	if err := c.resolveConfigContext(); err != nil {
		return nil
	}

	return c.options.configContext
}

// GetEndpoints returns the client's endpoints from the override set with WithEndpoints
// or from the configuration.
func (c *Client) GetEndpoints() []string {
	if c.options.unixSocketPath != "" {
		return []string{c.options.unixSocketPath}
	}

	if len(c.options.endpointsOverride) > 0 {
		return c.options.endpointsOverride
	}

	if c.options.config != nil {
		if err := c.resolveConfigContext(); err != nil {
			return nil
		}

		return c.options.configContext.Endpoints
	}

	return nil
}

// GetClusterName returns the client's cluster name from the override set with WithClustername
// or from the configuration.
func (c *Client) GetClusterName() string {
	if c.options.clusterNameOverride != "" {
		return c.options.clusterNameOverride
	}

	if c.options.config != nil {
		if err := c.resolveConfigContext(); err != nil {
			return ""
		}

		if c.options.configContext != nil {
			return c.options.configContext.Cluster
		}
	}

	return ""
}

// New returns a new Client.
func New(ctx context.Context, opts ...OptionFunc) (c *Client, err error) {
	c = new(Client)

	c.options = new(Options)

	for _, opt := range opts {
		if err = opt(c.options); err != nil {
			return nil, err
		}
	}

	if len(c.GetEndpoints()) < 1 {
		return nil, errors.New("failed to determine endpoints")
	}

	c.conn, err = c.getConn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create client connection: %w", err)
	}

	c.MachineClient = machineapi.NewMachineServiceClient(c.conn)
	c.TimeClient = timeapi.NewTimeServiceClient(c.conn)
	c.ClusterClient = clusterapi.NewClusterServiceClient(c.conn)
	c.StorageClient = storageapi.NewStorageServiceClient(c.conn)
	c.ResourceClient = resourceapi.NewResourceServiceClient(c.conn) //nolint:staticcheck
	c.InspectClient = inspectapi.NewInspectServiceClient(c.conn)

	c.Resources = &ResourcesClient{c.ResourceClient}
	c.Inspect = &InspectClient{c.InspectClient}
	c.COSI = state.WrapCore(client.NewAdapter(cosiv1alpha1.NewStateClient(c.conn)))

	return c, nil
}

// Close shuts down client protocol.
func (c *Client) Close() error {
	return c.conn.Close()
}

// KubeconfigRaw returns K8s client config (kubeconfig).
func (c *Client) KubeconfigRaw(ctx context.Context) (io.ReadCloser, <-chan error, error) {
	stream, err := c.MachineClient.Kubeconfig(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, nil, err
	}

	return ReadStream(stream)
}

func (c *Client) extractKubeconfig(r io.ReadCloser) ([]byte, error) {
	defer r.Close() //nolint:errcheck

	gzR, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	// returned .tar.gz should contain only single file (kubeconfig)
	var kubeconfigBuf bytes.Buffer

	tar := tar.NewReader(gzR)

	for {
		_, err = tar.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		_, err = io.Copy(&kubeconfigBuf, tar)
		if err != nil {
			return nil, err
		}
	}

	if err = gzR.Close(); err != nil {
		return nil, err
	}

	return kubeconfigBuf.Bytes(), nil
}

// Kubeconfig returns K8s client config (kubeconfig).
func (c *Client) Kubeconfig(ctx context.Context) ([]byte, error) {
	r, errCh, err := c.KubeconfigRaw(ctx)
	if err != nil {
		return nil, err
	}

	kubeconfig, err := c.extractKubeconfig(r)

	if err2 := <-errCh; err2 != nil {
		// prefer errCh (error from server) as if server failed,
		// extractKubeconfig failed as well, but server failure is more descriptive
		return nil, err2
	}

	return kubeconfig, err
}

// ApplyConfiguration implements proto.MachineServiceClient interface.
func (c *Client) ApplyConfiguration(ctx context.Context, req *machineapi.ApplyConfigurationRequest, callOptions ...grpc.CallOption) (resp *machineapi.ApplyConfigurationResponse, err error) {
	resp, err = c.MachineClient.ApplyConfiguration(ctx, req, callOptions...)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ApplyConfigurationResponse) //nolint:errcheck

	return
}

// GenerateConfiguration implements proto.MachineServiceClient interface.
func (c *Client) GenerateConfiguration(ctx context.Context, req *machineapi.GenerateConfigurationRequest, callOptions ...grpc.CallOption) (resp *machineapi.GenerateConfigurationResponse, err error) {
	resp, err = c.MachineClient.GenerateConfiguration(ctx, req, callOptions...)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.GenerateConfigurationResponse) //nolint:errcheck

	return
}

// Disks returns the list of block devices.
func (c *Client) Disks(ctx context.Context, callOptions ...grpc.CallOption) (resp *storageapi.DisksResponse, err error) {
	resp, err = c.StorageClient.Disks(ctx, &emptypb.Empty{}, callOptions...)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*storageapi.DisksResponse) //nolint:errcheck

	return
}

// Stats implements the proto.MachineServiceClient interface.
func (c *Client) Stats(ctx context.Context, namespace string, driver common.ContainerDriver, callOptions ...grpc.CallOption) (resp *machineapi.StatsResponse, err error) {
	resp, err = c.MachineClient.Stats(
		ctx, &machineapi.StatsRequest{
			Namespace: namespace,
			Driver:    driver,
		},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.StatsResponse) //nolint:errcheck

	return
}

// Containers implements the proto.MachineServiceClient interface.
func (c *Client) Containers(ctx context.Context, namespace string, driver common.ContainerDriver, callOptions ...grpc.CallOption) (resp *machineapi.ContainersResponse, err error) {
	resp, err = c.MachineClient.Containers(
		ctx,
		&machineapi.ContainersRequest{
			Namespace: namespace,
			Driver:    driver,
		},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ContainersResponse) //nolint:errcheck

	return
}

// Restart implements the proto.MachineServiceClient interface.
func (c *Client) Restart(ctx context.Context, namespace string, driver common.ContainerDriver, id string, callOptions ...grpc.CallOption) (err error) {
	resp, err := c.MachineClient.Restart(ctx, &machineapi.RestartRequest{
		Id:        id,
		Namespace: namespace,
		Driver:    driver,
	})

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return
}

// Reset implements the proto.MachineServiceClient interface.
func (c *Client) Reset(ctx context.Context, graceful, reboot bool) (err error) {
	resp, err := c.MachineClient.Reset(ctx, &machineapi.ResetRequest{Graceful: graceful, Reboot: reboot})

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return
}

// ResetGeneric implements the proto.MachineServiceClient interface.
func (c *Client) ResetGeneric(ctx context.Context, req *machineapi.ResetRequest) error {
	_, err := c.ResetGenericWithResponse(ctx, req)

	return err
}

// ResetGenericWithResponse resets the machine and returns the response.
func (c *Client) ResetGenericWithResponse(ctx context.Context, req *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	resp, err := c.MachineClient.Reset(ctx, req)

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return resp, err
}

// RebootMode provides various mode through which the reboot process can be done.
type RebootMode func(*machineapi.RebootRequest)

// WithPowerCycle option runs the Reboot fun in powercycle mode.
func WithPowerCycle(req *machineapi.RebootRequest) {
	req.Mode = machineapi.RebootRequest_POWERCYCLE
}

// Reboot implements the proto.MachineServiceClient interface.
func (c *Client) Reboot(ctx context.Context, opts ...RebootMode) error {
	_, err := c.RebootWithResponse(ctx, opts...)

	return err
}

// RebootWithResponse reboots the machine and returns the response.
func (c *Client) RebootWithResponse(ctx context.Context, opts ...RebootMode) (*machineapi.RebootResponse, error) {
	var req machineapi.RebootRequest
	for _, opt := range opts {
		opt(&req)
	}

	resp, err := c.MachineClient.Reboot(ctx, &req)

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return resp, err
}

// Rollback implements the proto.MachineServiceClient interface.
func (c *Client) Rollback(ctx context.Context) (err error) {
	resp, err := c.MachineClient.Rollback(ctx, &machineapi.RollbackRequest{})

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return
}

// Bootstrap implements the proto.MachineServiceClient interface.
func (c *Client) Bootstrap(ctx context.Context, req *machineapi.BootstrapRequest) (err error) {
	resp, err := c.MachineClient.Bootstrap(ctx, req)

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return
}

// ShutdownOption provides shutdown API options.
type ShutdownOption func(*machineapi.ShutdownRequest)

// WithShutdownForce forces the shutdown even if the Kubernetes API is down.
func WithShutdownForce(force bool) ShutdownOption {
	return func(req *machineapi.ShutdownRequest) {
		req.Force = force
	}
}

// Shutdown implements the proto.MachineServiceClient interface.
func (c *Client) Shutdown(ctx context.Context, opts ...ShutdownOption) error {
	_, err := c.ShutdownWithResponse(ctx, opts...)

	return err
}

// ShutdownWithResponse shuts down the machine and returns the response.
func (c *Client) ShutdownWithResponse(ctx context.Context, opts ...ShutdownOption) (*machineapi.ShutdownResponse, error) {
	var req machineapi.ShutdownRequest

	for _, opt := range opts {
		opt(&req)
	}

	resp, err := c.MachineClient.Shutdown(ctx, &req)

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return resp, err
}

// Dmesg implements the proto.MachineServiceClient interface.
func (c *Client) Dmesg(ctx context.Context, follow, tail bool) (machineapi.MachineService_DmesgClient, error) {
	return c.MachineClient.Dmesg(ctx, &machineapi.DmesgRequest{
		Follow: follow,
		Tail:   tail,
	})
}

// Logs implements the proto.MachineServiceClient interface.
func (c *Client) Logs(ctx context.Context, namespace string, driver common.ContainerDriver, id string, follow bool, tailLines int32) (stream machineapi.MachineService_LogsClient, err error) {
	stream, err = c.MachineClient.Logs(ctx, &machineapi.LogsRequest{
		Namespace: namespace,
		Driver:    driver,
		Id:        id,
		Follow:    follow,
		TailLines: tailLines,
	})

	return
}

// Version implements the proto.MachineServiceClient interface.
func (c *Client) Version(ctx context.Context, callOptions ...grpc.CallOption) (resp *machineapi.VersionResponse, err error) {
	resp, err = c.MachineClient.Version(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.VersionResponse) //nolint:errcheck

	return
}

// Processes implements the proto.MachineServiceClient interface.
func (c *Client) Processes(ctx context.Context, callOptions ...grpc.CallOption) (resp *machineapi.ProcessesResponse, err error) {
	resp, err = c.MachineClient.Processes(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ProcessesResponse) //nolint:errcheck

	return
}

// Memory implements the proto.MachineServiceClient interface.
func (c *Client) Memory(ctx context.Context, callOptions ...grpc.CallOption) (resp *machineapi.MemoryResponse, err error) {
	resp, err = c.MachineClient.Memory(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.MemoryResponse) //nolint:errcheck

	return
}

// Mounts implements the proto.MachineServiceClient interface.
func (c *Client) Mounts(ctx context.Context, callOptions ...grpc.CallOption) (resp *machineapi.MountsResponse, err error) {
	resp, err = c.MachineClient.Mounts(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.MountsResponse) //nolint:errcheck

	return
}

// LS implements the proto.MachineServiceClient interface.
func (c *Client) LS(ctx context.Context, req *machineapi.ListRequest) (stream machineapi.MachineService_ListClient, err error) {
	return c.MachineClient.List(ctx, req)
}

// DiskUsage implements the proto.MachineServiceClient interface.
func (c *Client) DiskUsage(ctx context.Context, req *machineapi.DiskUsageRequest) (stream machineapi.MachineService_DiskUsageClient, err error) {
	return c.MachineClient.DiskUsage(ctx, req)
}

// Copy implements the proto.MachineServiceClient interface.
func (c *Client) Copy(ctx context.Context, rootPath string) (io.ReadCloser, <-chan error, error) {
	stream, err := c.MachineClient.Copy(ctx, &machineapi.CopyRequest{
		RootPath: rootPath,
	})
	if err != nil {
		return nil, nil, err
	}

	return ReadStream(stream)
}

// Upgrade initiates a Talos upgrade and implements the proto.MachineServiceClient interface.
func (c *Client) Upgrade(ctx context.Context, image string, preserve, stage, force bool, callOptions ...grpc.CallOption) (resp *machineapi.UpgradeResponse, err error) {
	resp, err = c.MachineClient.Upgrade(
		ctx,
		&machineapi.UpgradeRequest{
			Image:    image,
			Preserve: preserve,
			Stage:    stage,
			Force:    force,
		},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.UpgradeResponse) //nolint:errcheck

	return
}

// ServiceList returns list of services with their state.
func (c *Client) ServiceList(ctx context.Context, callOptions ...grpc.CallOption) (resp *machineapi.ServiceListResponse, err error) {
	resp, err = c.MachineClient.ServiceList(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ServiceListResponse) //nolint:errcheck

	return
}

// ServiceInfo provides info about a service and node metadata.
type ServiceInfo struct {
	Metadata *common.Metadata
	Service  *machineapi.ServiceInfo
}

// ServiceInfo returns info about a single service
//
// This is implemented via service list API, as we don't have many services
// If service with given id is not registered, function returns nil.
func (c *Client) ServiceInfo(ctx context.Context, id string, callOptions ...grpc.CallOption) (services []ServiceInfo, err error) {
	var resp *machineapi.ServiceListResponse

	resp, err = c.MachineClient.ServiceList(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	if err != nil {
		return services, err
	}

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ServiceListResponse) //nolint:errcheck

	// FilterMessages might remove responses if they actually contain errors,
	// errors will be merged into `resp`.
	if resp == nil {
		return services, err
	}

	for _, resp := range resp.Messages {
		for _, svc := range resp.Services {
			if svc.Id == id {
				services = append(services, ServiceInfo{
					Metadata: resp.Metadata,
					Service:  svc,
				})
			}
		}
	}

	return services, err
}

// ServiceStart starts a service.
func (c *Client) ServiceStart(ctx context.Context, id string, callOptions ...grpc.CallOption) (resp *machineapi.ServiceStartResponse, err error) {
	resp, err = c.MachineClient.ServiceStart(
		ctx,
		&machineapi.ServiceStartRequest{Id: id},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ServiceStartResponse) //nolint:errcheck

	return
}

// ServiceStop stops a service.
func (c *Client) ServiceStop(ctx context.Context, id string, callOptions ...grpc.CallOption) (resp *machineapi.ServiceStopResponse, err error) {
	resp, err = c.MachineClient.ServiceStop(
		ctx,
		&machineapi.ServiceStopRequest{Id: id},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ServiceStopResponse) //nolint:errcheck

	return
}

// ServiceRestart restarts a service.
func (c *Client) ServiceRestart(ctx context.Context, id string, callOptions ...grpc.CallOption) (resp *machineapi.ServiceRestartResponse, err error) {
	resp, err = c.MachineClient.ServiceRestart(
		ctx,
		&machineapi.ServiceRestartRequest{Id: id},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.ServiceRestartResponse) //nolint:errcheck

	return
}

// Time returns the time.
func (c *Client) Time(ctx context.Context, callOptions ...grpc.CallOption) (resp *timeapi.TimeResponse, err error) {
	resp, err = c.TimeClient.Time(
		ctx,
		&emptypb.Empty{},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*timeapi.TimeResponse) //nolint:errcheck

	return
}

// TimeCheck returns the time compared to the specified ntp server.
func (c *Client) TimeCheck(ctx context.Context, server string, callOptions ...grpc.CallOption) (resp *timeapi.TimeResponse, err error) {
	resp, err = c.TimeClient.TimeCheck(
		ctx,
		&timeapi.TimeRequest{Server: server},
		callOptions...,
	)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*timeapi.TimeResponse) //nolint:errcheck

	return
}

// Read reads a file.
func (c *Client) Read(ctx context.Context, path string) (io.ReadCloser, <-chan error, error) {
	stream, err := c.MachineClient.Read(ctx, &machineapi.ReadRequest{Path: path})
	if err != nil {
		return nil, nil, err
	}

	return ReadStream(stream)
}

// ClusterHealthCheck runs a Talos cluster health check.
func (c *Client) ClusterHealthCheck(ctx context.Context, waitTimeout time.Duration, clusterInfo *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error) {
	return c.ClusterClient.HealthCheck(ctx, &clusterapi.HealthCheckRequest{
		WaitTimeout: durationpb.New(waitTimeout),
		ClusterInfo: clusterInfo,
	})
}

// EtcdRemoveMember removes a node from etcd cluster.
func (c *Client) EtcdRemoveMember(ctx context.Context, req *machineapi.EtcdRemoveMemberRequest, callOptions ...grpc.CallOption) error {
	resp, err := c.MachineClient.EtcdRemoveMember(ctx, req, callOptions...)

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return err
}

// EtcdLeaveCluster makes node leave etcd cluster.
func (c *Client) EtcdLeaveCluster(ctx context.Context, req *machineapi.EtcdLeaveClusterRequest, callOptions ...grpc.CallOption) error {
	resp, err := c.MachineClient.EtcdLeaveCluster(ctx, req, callOptions...)

	if err == nil {
		_, err = FilterMessages(resp, err)
	}

	return err
}

// EtcdForfeitLeadership makes node forfeit leadership in the etcd cluster.
func (c *Client) EtcdForfeitLeadership(ctx context.Context, req *machineapi.EtcdForfeitLeadershipRequest, callOptions ...grpc.CallOption) (*machineapi.EtcdForfeitLeadershipResponse, error) {
	resp, err := c.MachineClient.EtcdForfeitLeadership(ctx, req, callOptions...)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.EtcdForfeitLeadershipResponse) //nolint:errcheck

	return resp, err
}

// EtcdMemberList lists etcd members of the cluster.
func (c *Client) EtcdMemberList(ctx context.Context, req *machineapi.EtcdMemberListRequest, callOptions ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	resp, err := c.MachineClient.EtcdMemberList(ctx, req, callOptions...)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.EtcdMemberListResponse) //nolint:errcheck

	return resp, err
}

// EtcdSnapshot receives a snapshot of the etcd from the node.
func (c *Client) EtcdSnapshot(ctx context.Context, req *machineapi.EtcdSnapshotRequest, callOptions ...grpc.CallOption) (io.ReadCloser, <-chan error, error) {
	stream, err := c.MachineClient.EtcdSnapshot(ctx, req, callOptions...)
	if err != nil {
		return nil, nil, err
	}

	return ReadStream(stream)
}

// EtcdRecover uploads etcd snapshot created with EtcdSnapshot to the node.
func (c *Client) EtcdRecover(ctx context.Context, snapshot io.Reader, callOptions ...grpc.CallOption) (*machineapi.EtcdRecoverResponse, error) {
	cli, err := c.MachineClient.EtcdRecover(ctx, callOptions...)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var n int

		n, err = snapshot.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("error reading snapshot: %w", err)
		}

		if err = cli.Send(&common.Data{
			Bytes: buf[:n],
		}); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, err
		}
	}

	resp, err := cli.CloseAndRecv()

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.EtcdRecoverResponse) //nolint:errcheck

	return resp, err
}

// GenerateClientConfiguration implements proto.MachineServiceClient interface.
func (c *Client) GenerateClientConfiguration(ctx context.Context, req *machineapi.GenerateClientConfigurationRequest, callOptions ...grpc.CallOption) (resp *machineapi.GenerateClientConfigurationResponse, err error) { //nolint:lll
	resp, err = c.MachineClient.GenerateClientConfiguration(ctx, req, callOptions...)

	var filtered interface{}
	filtered, err = FilterMessages(resp, err)
	resp, _ = filtered.(*machineapi.GenerateClientConfigurationResponse) //nolint:errcheck

	return
}

// PacketCapture implements the proto.MachineServiceClient interface.
func (c *Client) PacketCapture(ctx context.Context, req *machineapi.PacketCaptureRequest) (io.ReadCloser, <-chan error, error) {
	stream, err := c.MachineClient.PacketCapture(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	return ReadStream(stream)
}

// MachineStream is a common interface for streams returned by streaming APIs.
type MachineStream interface {
	Recv() (*common.Data, error)
	grpc.ClientStream
}

// ReadStream converts grpc stream into io.Reader.
//
//nolint:gocyclo
func ReadStream(stream MachineStream) (io.ReadCloser, <-chan error, error) {
	errCh := make(chan error, 1)
	pr, pw := io.Pipe()

	go func() {
		//nolint:errcheck
		defer pw.Close()
		defer close(errCh)

		for {
			data, err := stream.Recv()
			if err != nil {
				if err == io.EOF || StatusCode(err) == codes.Canceled || StatusCode(err) == codes.DeadlineExceeded {
					return
				}
				//nolint:errcheck
				pw.CloseWithError(err)

				return
			}

			if data.Bytes != nil {
				_, err = pw.Write(data.Bytes)
				if err != nil {
					return
				}
			}

			if data.Metadata != nil && data.Metadata.Error != "" {
				if data.Metadata.Status != nil {
					errCh <- status.FromProto(data.Metadata.Status).Err()
				} else {
					errCh <- errors.New(data.Metadata.Error)
				}
			}
		}
	}()

	return pr, errCh, stream.CloseSend()
}
