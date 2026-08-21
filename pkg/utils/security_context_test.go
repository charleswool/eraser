package utils

import "testing"

// The identity is a hardware-verified constraint, not a preference: on an AKS
// Windows node containerd's pipe ACL refuses both LocalService and
// NetworkService. A later hardening pass that lowers it would compile and pass
// review, and only fail on a real node.
func TestWindowsPodSecurityContextRunsAsSystemHostProcess(t *testing.T) {
	opts := WindowsPodSecurityContext.WindowsOptions
	if opts == nil {
		t.Fatal("WindowsOptions is nil")
	}

	if opts.HostProcess == nil || !*opts.HostProcess {
		t.Error("HostProcess must be true; the worker cannot reach the CRI pipe otherwise")
	}

	const want = `NT AUTHORITY\SYSTEM`
	if opts.RunAsUserName == nil || *opts.RunAsUserName != want {
		t.Errorf("RunAsUserName = %v, want %q", opts.RunAsUserName, want)
	}
}
