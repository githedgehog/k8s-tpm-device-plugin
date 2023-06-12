package plugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	socketName = "tpm.sock"
)

var (
	connectionTimeout = time.Second * 5
	registerTimeout   = time.Second * 30
)

type tpmDevicePlugin struct {
	l          *zap.Logger
	socketPath string
	server     *grpc.Server
}

var _ Interface = &tpmDevicePlugin{}
var _ pluginapi.DevicePluginServer = &tpmDevicePlugin{}

func NewTPMDevicePlugin(l *zap.Logger) (Interface, error) {
	return &tpmDevicePlugin{
		socketPath: filepath.Join(pluginapi.DevicePluginPath, socketName),
		// will be initialized by Start()
		server: nil,
	}, nil
}

func (p *tpmDevicePlugin) init() {
	p.server = grpc.NewServer()
}

func (p *tpmDevicePlugin) cleanup() {
	p.server = nil
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
	if err := p.Register(ctx); err != nil {
		return err
	}

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
	conn, err := grpc.DialContext(subCtx, p.socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
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
	conn, err := grpc.DialContext(connCtx, pluginapi.KubeletSocket, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("connecting to kubelet socket at %s: %w", pluginapi.KubeletSocket, err)
	}

	client := pluginapi.NewRegistrationClient(conn)

	regCtx, regCancel := context.WithTimeout(ctx, registerTimeout)
	defer regCancel()
	if _, err := client.Register(regCtx, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     socketName,
		ResourceName: "githedgehog.com/tpmrm",
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
func (*tpmDevicePlugin) Allocate(context.Context, *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	panic("unimplemented")
}

// GetDevicePluginOptions implements v1beta1.DevicePluginServer
func (*tpmDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	panic("unimplemented")
}

// GetPreferredAllocation implements v1beta1.DevicePluginServer
func (*tpmDevicePlugin) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	panic("unimplemented")
}

// ListAndWatch implements v1beta1.DevicePluginServer
func (*tpmDevicePlugin) ListAndWatch(*pluginapi.Empty, pluginapi.DevicePlugin_ListAndWatchServer) error {
	panic("unimplemented")
}

// PreStartContainer implements v1beta1.DevicePluginServer
func (*tpmDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	panic("unimplemented")
}
