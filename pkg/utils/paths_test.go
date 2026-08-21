package utils

import "testing"

// On Windows the handoff endpoints are Unix domain sockets, so the mount path
// plus an endpoint name has to fit in sun_path. Overrunning it fails inside
// bind() with "invalid argument" on a node, which is a long way from whoever
// lengthens this constant.
func TestWindowsSharedDataMountPathFitsInSunPath(t *testing.T) {
	const (
		maxSocketPath   = 107
		longestEndpoint = `\eraseCompleteCollect`
	)

	if got := len(SharedDataMountPathWindows + longestEndpoint); got > maxSocketPath {
		t.Errorf("longest endpoint path is %d bytes, over the %d byte sun_path limit", got, maxSocketPath)
	}
}
