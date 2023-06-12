package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/githedgehog/k8s-tpm-device-plugin/internal/plugin"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func main() {
	l := zap.Must(NewLogger(zapcore.DebugLevel, "console", true))
	run(context.Background(), l)
}

func run(ctx context.Context, l *zap.Logger) error {
	// some of this code has been borrowed by the NVIDIA plugin: https://github.com/NVIDIA/k8s-device-plugin
	// watch the kubelet for restarts, we do this like other plugins by looking for the kubelet socket to be recreated
	// this means that we will have to restart our plugin.
	// NOTE: the restart is necessary as we need to register with the kubelet every time
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: initializing watcher: %w", err)
	}
	if err := fsw.Add(pluginapi.KubeletSocket); err != nil {
		return fmt.Errorf("fsnotify: failed to add %s to files we need to watch: %w", pluginapi.KubeletSocket, err)
	}

	// subscribe to OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	p, err := plugin.NewTPMDevicePlugin(l)
	if err != nil {
		return fmt.Errorf("TPM Device plugin create: %w", err)
	}
	// start plugin
	if err := p.Start(ctx); err != nil {
		return fmt.Errorf("TPM Device plugin failed to start on startup: %w", err)
	}

runLoop:
	for {
		// now watch for events and react to them
		select {
		case event := <-fsw.Events:
			if event.Name == pluginapi.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				l.Info("fsnotifiy: kubelet socket created, restarting...", zap.String("kubeletSocket", pluginapi.KubeletSocket))
				if err := restart(ctx, p); err != nil {
					return err
				}
			}
		case err := <-fsw.Errors:
			l.Warn("fsnotify error", zap.Error(err))

		// watch for OS signals. SIGHUP means a restart. Any other registered signals signal a shutdown
		case s := <-sigCh:
			switch s {
			case syscall.SIGHUP:
				l.Info("SIGHUP signal received, restarting...")
				if err := restart(ctx, p); err != nil {
					return err
				}
			default:
				l.Info("Signal received, shutting down...", zap.String("signal", s.String()))
				break runLoop
			}
		}
	}

	// stop plugin on regular shutdown
	if err := p.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop TPM device plugin on shutdown: %w", err)
	}

	return nil
}

func restart(ctx context.Context, p plugin.Interface) error {
	if err := p.Start(ctx); err != nil {
		return fmt.Errorf("TPM Device plugin failed to start on restart: %w", err)
	}
	if err := p.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop TPM device plugin on restart: %w", err)
	}
	return nil
}
