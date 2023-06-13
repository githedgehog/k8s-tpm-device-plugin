package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"go.githedgehog.com/k8s-tpm-device-plugin/internal/plugin"
	"go.githedgehog.com/k8s-tpm-device-plugin/internal/plugin/tpmrm"
	"go.githedgehog.com/k8s-tpm-device-plugin/pkg/version"

	"github.com/fsnotify/fsnotify"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var (
	defaultLogLevel = zapcore.InfoLevel
)

var description = `
This is a Kubernetes TPM device plugin. Its purpose is to pass through the TPM
device(s) from the host without the need of requiring to run a privileged pod.

There are currently two devices which are of interest for that:
- /dev/tpmrm0
- /dev/tpm0

The former is capable of being accessed by multiple processes and users and
it uses the in-kernel resource manager to facilitate that. Technically, there
is no limit on how many of these devices can be passed through to pods.
However, the Kubernetes scheduler requires to register IDs, so by default
this plugin simply registers N number of devices for that. See the CLI flags
on how to override this.

The /dev/tpm0 device on the contrary can only be accessed by one process in
total. It is usually not really being used nowadays, however, if kernels are
too old (<4.12) and you still want to make use of this device, then this is the
device that you should request. However, ensure that the device is not being
used on the host itself. For example, the 'tpm2-abrmd' was previously managing
access to TPMs.

Use *ONE* of the following to resource request limits in a Kubernetes pod to
get access to the device - preferrable as just explained the first one, and
only in extraordinary circumstances the second one:
- githedgehog.com/tpmrm: 1
- githedgehog.com/tpm: 1
`

func main() {
	app := &cli.App{
		Name:        "k8s-tpm-device-plugin",
		Usage:       "Kubernetes TPM device plugin",
		UsageText:   "k8s-tpm-device-plugin",
		Description: description[1 : len(description)-1],
		Version:     version.Version,
		Flags: []cli.Flag{
			&cli.GenericFlag{
				Name:    "log-level",
				Usage:   "minimum log level to log at",
				Value:   &defaultLogLevel,
				EnvVars: []string{"LOG_LEVEL"},
			},
			&cli.StringFlag{
				Name:    "log-format",
				Usage:   "log format to use: json or console",
				Value:   "json",
				EnvVars: []string{"LOG_FORMAT"},
			},
			&cli.BoolFlag{
				Name:    "log-development",
				Usage:   "enables development log settings",
				Value:   false,
				EnvVars: []string{"LOG_DEVELOPMENT"},
			},
			&cli.UintFlag{
				Name:    "num-tpmrm-devices",
				Usage:   "number of artificial /dev/tpmrm0 devices to communicate to the kubelet",
				Value:   64, // yes, I totally randomly made up that number
				EnvVars: []string{"NUM_TPMRM_DEVICES"},
			},
			&cli.BoolFlag{
				Name:    "pass-tpm2tools-tcti-env-var",
				Usage:   "passes a TPM2TOOLS_TCTI environment variable to the injected pods which points to the device",
				Value:   false,
				EnvVars: []string{"PASS_TPM2TOOLS_TCTI_ENV_VAR"},
			},
		},
		Action: func(ctx *cli.Context) error {
			// initialize logger
			l := zap.Must(NewLogger(
				*ctx.Generic("log-level").(*zapcore.Level),
				ctx.String("log-format"),
				ctx.Bool("log-development"),
			))
			defer func() {
				if err := l.Sync(); err != nil {
					l.Debug("Flushing logger failed", zap.Error(err))
				}
			}()
			zap.ReplaceGlobals(l)

			// run the application
			if err := run(ctx, l); err != nil {
				l.Panic("k8s-tpm-device-plugin failed", zap.Error(err))
			}
			return nil
		},
	}

	// that should be caught by the logger as it panics, but if the logger implementation changes
	// then this is not guaranteed, so this is a nice safe-guard
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
}

func run(cliCtx *cli.Context, l *zap.Logger) error {
	ctx := cliCtx.Context

	// print the version information
	l.Info("Starting k8s-tpm-device-plugin", zap.String("version", version.Version), zap.String("go", runtime.Version()))

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

	p, err := tpmrm.New(l, cliCtx.Uint("num-tpmrm-devices"), cliCtx.Bool("pass-tpm2tools-tcti-env-var"))
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
