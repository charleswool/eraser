// Command ipcspike exercises the worker handoff in pkg/utils across two
// containers sharing a volume, which is the topology the collector, scanner and
// remover actually run in. It is a fork-only harness, not part of the product.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eraser-dev/eraser/api/unversioned"
	util "github.com/eraser-dev/eraser/pkg/utils"
)

var (
	role = flag.String("role", "", "producer or consumer")
	dir  = flag.String("dir", `C:\eraser-shared`, "directory holding the handoff endpoints")
)

func main() {
	flag.Parse()

	d := resolveDir(*dir)
	fmt.Printf("%-10s dir            : %s\n", *role, d)

	imagesPath := filepath.Join(d, "collectScan")
	completePath := filepath.Join(d, "eraseComplete")
	absentPath := filepath.Join(d, "noSuchScanner")

	// not deferred: every failure path below is os.Exit, which would skip it
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

	var err error
	switch *role {
	case "producer":
		err = producer(ctx, imagesPath, completePath)
	case "consumer":
		err = consumer(ctx, imagesPath, completePath, absentPath)
	default:
		err = fmt.Errorf("-role must be producer or consumer")
	}
	cancel()

	if err != nil {
		fmt.Printf("RESULT %s: FAIL %v\n", *role, err)
		os.Exit(1)
	}
	fmt.Printf("RESULT %s: PASS\n", *role)
}

// producer stands in for the collector: hand the image list over, then wait to
// be told the erase finished.
func producer(ctx context.Context, imagesPath, completePath string) error {
	images := []unversioned.Image{
		{ImageID: "sha256:aaa", Names: []string{"mcr.microsoft.com/windows/servercore:ltsc2022"}},
		{ImageID: "sha256:bbb", Names: []string{"mcr.microsoft.com/windows/nanoserver:ltsc2022"}},
	}

	// published before the write so the peer cannot miss it
	completion, err := util.CreateCompletionPipe(completePath)
	if err != nil {
		return fmt.Errorf("create completion: %w", err)
	}
	defer func() { _ = completion.Close() }()

	start := time.Now()
	if err := util.WriteImagesPipe(ctx, imagesPath, images); err != nil {
		return fmt.Errorf("write images: %w", err)
	}
	fmt.Printf("producer   WriteImagesPipe: OK %d images in %s\n", len(images), took(start))

	start = time.Now()
	payload, err := completion.Await(ctx)
	if err != nil {
		return fmt.Errorf("await completion: %w", err)
	}
	fmt.Printf("producer   Await          : OK %q after %s\n", string(payload), took(start))

	if string(payload) != util.EraseCompleteMessage {
		return fmt.Errorf("payload %q, want %q", payload, util.EraseCompleteMessage)
	}
	return nil
}

// consumer stands in for the remover: read the list, then signal completion. It
// also checks that signaling an endpoint nobody published is distinguishable,
// since that is how a disabled scanner is detected.
func consumer(ctx context.Context, imagesPath, completePath, absentPath string) error {
	start := time.Now()
	images, err := util.ReadImagesPipe(ctx, imagesPath)
	if err != nil {
		return fmt.Errorf("read images: %w", err)
	}
	fmt.Printf("consumer   ReadImagesPipe : OK %d images in %s\n", len(images), took(start))
	for _, img := range images {
		fmt.Printf("consumer     %s %v\n", img.ImageID, img.Names)
	}

	if err := util.WriteCompletionPipe(ctx, absentPath); err == nil {
		return fmt.Errorf("signaling an unpublished endpoint should have failed")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("unpublished endpoint gave %v, want an IsNotExist error", err)
	}
	fmt.Printf("consumer   absent peer    : OK reported as IsNotExist\n")

	if err := util.WriteCompletionPipe(ctx, completePath); err != nil {
		return fmt.Errorf("write completion: %w", err)
	}
	fmt.Printf("consumer   WriteCompletion: OK\n")
	return nil
}

// resolveDir accounts for HostProcess containers, where volume mounts appear
// under the sandbox mount point rather than at the requested drive path.
func resolveDir(dir string) string {
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	if smp := os.Getenv("CONTAINER_SANDBOX_MOUNT_POINT"); smp != "" {
		if vol := filepath.VolumeName(dir); vol != "" {
			dir = dir[len(vol):]
		}
		if p := filepath.Join(smp, dir); p != "" {
			return p
		}
	}
	return dir
}

func took(start time.Time) time.Duration {
	return time.Since(start).Round(time.Millisecond)
}
