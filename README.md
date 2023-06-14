# k8s-tpm-device-plugin

This is a Kubernetes device plugin to make TPM devices accessible from Kubernetes pods without the need to run pods in privileged mode.
The initial goal for this plugin was to enable the [rust keylime agent](https://github.com/keylime/rust-keylime/) to run on Kubernetes.

## Overview

In a nutshell the plugin consists actually of two plugins which allows you to pass through the `/dev/tpmrm0` or `/dev/tpm0` devices.
Using the former device is preferred, and the latter one should usually only be used if the Linux kernel version of the cluster is <4.12.

Note that particularly when you are relying on the `/dev/tpm0` device that the host is not already holding full access to it.
This could be the case if you are running [tpm2-abrmd](https://github.com/tpm2-software/tpm2-abrmd) which is not recommended any longer.

As mentioned below only one pod can hold the `/dev/tpm0` device at a time.
Up to _N_ pods on a host can gain access to the `/dev/tpmrm0` device.
This value is configurable and can be overwritten at installation time with for example an additional command-line flag while installing the helm chart: `--set pluginSettings.numTpmRmDevices=128`.
By default up to 64 pods can gain access to the `/dev/tpmrm0` device on a host.
Note that this number is totally arbitrary, and can unfortunately not be handled differently because of the way how devices are allocated by the Kubernetes device manager.

## Installation

The TPM device plugin must be deployed as a Kubernetes DaemonSet.
It comes packaged as a helm chart.
Run the following to deploy this helm chart in your Kubernetes cluster

```bash
helm upgrade --install hhtpmplugin oci://ghcr.io/githedgehog/k8s-tpm-device-plugin/helm-charts/k8s-tpm-device-plugin
```

If you want (or need) to make modifications to the installation, take a look at the [values.yaml](https://github.com/githedgehog/k8s-tpm-device-plugin/blob/main/build/helm/k8s-tpm-device-plugin/values.yaml) file.

## Usage

This is the preferred methodYou can request the `/dev/tpmrm0` device like the following in the resource limits section of a container spec:

```yaml
    resources:
      limits:
        githedgehog.com/tpmrm: 1
```

In edge cases, and when you truly need it, you can similarly request the `/dev/tpm0` device like this (_NOTE: not implemented yet!_):

```yaml
    resources:
      limits:
        githedgehog.com/tpm: 1
```

**NOTE:** The `/dev/tpm0` device can always be allocated only to one pod on a host at the same time.
It is generally not advisable to use this device at all if your Linux kernel has support for the `/dev/tpmrm0` device.

## Example

Here is a full pod yaml example which provides full access to the TPM device without the need for any elevated privileges or capabilities:

```yaml
---
apiVersion: v1
kind: Pod
metadata:
  name: tpm-device-test
spec:
  terminationGracePeriodSeconds: 1
  containers:
  - name: tpm-device-test
    image: fedora:latest
    command:
    - /bin/bash
    - -c
    - while true; do sleep 3600; done
    resources:
      limits:
        githedgehog.com/tpmrm: 1
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
```

You can test this pod by running the following commands:

```bash
kubectl exec -ti tpm-device-test -- /bin/bash

dnf install -y tpm2-tools
tpm2_getcap --list
tpm2_getrandom --hex 16
```
