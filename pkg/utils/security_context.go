package utils

import (
	corev1 "k8s.io/api/core/v1"
)

var trueval = true

var SharedSecurityContext = &corev1.SecurityContext{
	Capabilities: &corev1.Capabilities{
		Drop: []corev1.Capability{"ALL"},
	},
	ReadOnlyRootFilesystem: &trueval,
	SeccompProfile: &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	},
}

// containerd's pipe ACL refuses LocalService and NetworkService, so SYSTEM is
// the only identity on a Windows node that can reach the runtime.
var windowsHostProcessUser = `NT AUTHORITY\SYSTEM`

// WindowsPodSecurityContext is pod-scoped rather than per-container because the
// API requires HostProcess to be identical across every container in the pod.
// Callers must also set PodSpec.HostNetwork, which HostProcess requires, and
// leave the container-level SecurityContext nil: SharedSecurityContext is built
// entirely from fields Windows does not support.
var WindowsPodSecurityContext = &corev1.PodSecurityContext{
	WindowsOptions: &corev1.WindowsSecurityContextOptions{
		HostProcess:   &trueval,
		RunAsUserName: &windowsHostProcessUser,
	},
}
