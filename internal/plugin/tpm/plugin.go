/*
Copyright 2023 Hedgehog SONiC Foundation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tpm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.githedgehog.com/k8s-tpm-device-plugin/internal/plugin"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	tpmID         = "tpm0"
	tpmSocketName = "hh-tpm.sock"
)

var (
	connectionTimeout = time.Second * 5
	registerTimeout   = time.Second * 30
	errUnimplmented   = errors.New("plugin does not implement this method")
)

func UnimplementedError(str string) error {
	return fmt.Errorf("%w: %s", errUnimplmented, str)
}

type tpmDevicePlugin struct {
	l          *zap.Logger
	tctiEnvVar bool
	socketPath string
	server     *grpc.Server
	stopCh     chan struct{}
}

var _ plugin.Interface = &tpmDevicePlugin{}
var _ pluginapi.DevicePluginServer = &tpmDevicePlugin{}

func New(l *zap.Logger, tctiEnvVar bool) (plugin.Interface, error) {
	return &tpmDevicePlugin{
		l:          l.With(zap.String("plugin", "tpm")),
		tctiEnvVar: tctiEnvVar,
		socketPath: filepath.Join(pluginapi.DevicePluginPath, tpmSocketName),
		// will be initialized by Start()
		server: nil,
		stopCh: nil,
	}, nil
}

func (p *tpmDevicePlugin) init() {
	p.server = grpc.NewServer()
	p.stopCh = make(chan struct{})
}

func (p *tpmDevicePlugin) cleanup() {
	close(p.stopCh)
	p.server = nil
	p.stopCh = nil
}

func (p *tpmDevicePlugin) Name() string {
	return "tpm"
}

// Start implements Interface
func (p *tpmDevicePlugin) Start(ctx context.Context) error {
	// caller safeguard
	if p == nil {
		return nil
	}
	p.init()

	if err := p.Serve(ctx); err != nil {
		return err
	}
	p.l.Info("TPM Device Plugin server started")
	if err := p.Register(ctx); err != nil {
		return err
	}
	p.l.Info("TPM Device Plugin registered with kubelet")

	return nil
}

// Stop implements Interface
func (p *tpmDevicePlugin) Stop(context.Context) error {
	// caller safeguard
	if p == nil || p.server == nil {
		return nil
	}
	p.l.Info("Stopping gRPC server", zap.String("socket", p.socketPath))
	p.server.Stop()
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing socket path %s: %w", p.socketPath, err)
	}
	p.cleanup()
	return nil
}

func (p *tpmDevicePlugin) Serve(ctx context.Context) error {
	// listen on unix socket
	// NOTE: no need to close the listener as the gRPC methods close the listener automatically
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing socket path %s: %w", p.socketPath, err)
	}
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "unix", p.socketPath)
	if err != nil {
		return fmt.Errorf("listening on unix socket %s: %w", p.socketPath, err)
	}
	p.l.Info("Listening on unix socket for gRPC server now", zap.String("socket", p.socketPath))

	// register the device plugin server API with the grpc server
	pluginapi.RegisterDevicePluginServer(p.server, p)

	// now run the gRPC server
	go func() {
		for {
			p.l.Info("Starting gRPC server now...")
			err := p.server.Serve(l)
			// err is nil when Stop() or GracefulStop() were called
			if err == nil {
				p.l.Info("Stopped gRPC server")
				return
			}
			p.l.Error("gRPC server crashed", zap.Error(err))
		}
	}()

	// connect to the gRPC server in blocking mode to ensure it is up before we return here
	subCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	conn, err := grpc.DialContext(subCtx, "unix:"+p.socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("gRPC server did not start within timeout %v: %w", connectionTimeout, err)
	}
	conn.Close() // nolint: errcheck

	p.l.Info("Started gRPC server")
	return nil
}

func (p *tpmDevicePlugin) Register(ctx context.Context) error {
	// connect to kubelet socket
	connCtx, connCancel := context.WithTimeout(ctx, connectionTimeout)
	defer connCancel()
	conn, err := grpc.DialContext(connCtx, "unix:"+pluginapi.KubeletSocket, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("connecting to kubelet socket at %s: %w", pluginapi.KubeletSocket, err)
	}

	client := pluginapi.NewRegistrationClient(conn)

	regCtx, regCancel := context.WithTimeout(ctx, registerTimeout)
	defer regCancel()
	if _, err := client.Register(regCtx, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     tpmSocketName,
		ResourceName: "githedgehog.com/tpm",
		Options: &pluginapi.DevicePluginOptions{
			PreStartRequired:                false,
			GetPreferredAllocationAvailable: false,
		},
	}); err != nil {
		return fmt.Errorf("gRPC register call: %w", err)
	}

	return nil
}

// Allocate implements v1beta1.DevicePluginServer
func (p *tpmDevicePlugin) Allocate(_ context.Context, allocateRequest *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	p.l.Debug("Allocate() call", zap.Reflect("allocateRequest", allocateRequest))
	resp := &pluginapi.AllocateResponse{}
	for _, req := range allocateRequest.ContainerRequests {
		p.l.Debug("allocate ContainerRequest", zap.Reflect("creq", req))
		var envs map[string]string
		if p.tctiEnvVar {
			envs = map[string]string{
				"TPM2TOOLS_TCTI": "device:/dev/tpm0",
			}
		}
		cresp := &pluginapi.ContainerAllocateResponse{
			Envs: envs,
			Devices: []*pluginapi.DeviceSpec{
				{
					ContainerPath: "/dev/tpm0",
					HostPath:      "/dev/tpm0",
					Permissions:   "rwm",
				},
			},
		}
		resp.ContainerResponses = append(resp.ContainerResponses, cresp)
	}
	return resp, nil
}

// GetDevicePluginOptions implements v1beta1.DevicePluginServer
func (*tpmDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired:                false,
		GetPreferredAllocationAvailable: false,
	}, nil
}

// GetPreferredAllocation implements v1beta1.DevicePluginServer
func (p *tpmDevicePlugin) GetPreferredAllocation(_ context.Context, _ *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	p.l.Debug("GetPreferredAllocation() is unimplemented for this plugin")
	return nil, UnimplementedError("GetPreferredAllocation")
}

// ListAndWatch implements v1beta1.DevicePluginServer
func (p *tpmDevicePlugin) ListAndWatch(_ *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	s.Send(&pluginapi.ListAndWatchResponse{Devices: []*pluginapi.Device{
		{
			ID:     tpmID,
			Health: pluginapi.Healthy,
		},
	}})

	// TODO: there is nothing we are doing at the moment to check if the TPM is healthy or not
	<-p.stopCh

	return nil
}

// PreStartContainer implements v1beta1.DevicePluginServer
func (p *tpmDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	p.l.Debug("PreStartContainer() is unimplemented for this plugin")
	return &pluginapi.PreStartContainerResponse{}, nil
}
