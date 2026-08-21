package utils

// The worker containers and the manager have to agree on where the shared-data
// volume is mounted, but they cannot share a single build-tagged constant: the
// manager is compiled for Linux and still has to emit pod specs for Windows
// nodes, so it needs both values at once.
//
// Callers in the manager must treat these as opaque strings. Joining a Windows
// sub-path with filepath.Join in a Linux-compiled binary emits forward slashes
// and produces a path that fails only at runtime, on the node.
const (
	SharedDataMountPathLinux   = "/run/eraser.sh/shared-data"
	SharedDataMountPathWindows = `C:\run\eraser.sh\shared-data`
)
