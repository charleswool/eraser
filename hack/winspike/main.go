// Windows CRI checker used by the fork-only windows-e2e workflow. Runs inside a
// HostProcess pod and exercises eraser's own pkg/cri over the containerd named pipe.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/eraser-dev/eraser/pkg/cri"
	util "github.com/eraser-dev/eraser/pkg/utils"
)

func main() {
	endpoint := flag.String("endpoint", util.CRIPath, "CRI endpoint")
	remove := flag.String("remove", "", "image ID or ref to delete")
	deleteOne := flag.Bool("delete-largest-unused", false, "delete the largest unused image and assert it is gone")
	timeout := flag.Duration("timeout", 15*time.Minute, "overall timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Printf("endpoint       : %s\n", *endpoint)
	fmt.Printf("default CRIPath: %s\n", util.CRIPath)

	dialStart := time.Now()
	client, err := cri.NewRemoverClient(*endpoint)
	if err != nil {
		fail("dial: %v", err)
	}
	fmt.Printf("dial           : OK (%s)\n", time.Since(dialStart).Round(time.Millisecond))

	images, err := client.ListImages(ctx)
	if err != nil {
		fail("ListImages: %v", err)
	}
	var total uint64
	for _, img := range images {
		total += img.Size_
	}
	fmt.Printf("ListImages     : OK %d images, %.2f GB\n", len(images), gb(total))

	containers, err := client.ListContainers(ctx)
	if err != nil {
		fail("ListContainers: %v", err)
	}
	fmt.Printf("ListContainers : OK %d containers\n", len(containers))

	unused := unusedImages(images, containers)
	sort.Slice(unused, func(i, j int) bool { return unused[i].Size_ > unused[j].Size_ })
	fmt.Printf("unused images  : %d\n", len(unused))
	for i, img := range unused {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(unused)-i)
			break
		}
		fmt.Printf("  %-70s %6.2f GB\n", name(img), gb(img.Size_))
	}

	target := *remove
	if *deleteOne {
		if len(unused) == 0 {
			fail("delete-largest-unused: no unused images on this node")
		}
		target = name(unused[0])
	}
	if target == "" {
		fmt.Println("no delete target; skipping")
		return
	}

	delStart := time.Now()
	if err := client.DeleteImage(ctx, target); err != nil {
		fail("DeleteImage %q after %s: %v", target, time.Since(delStart).Round(time.Second), err)
	}
	elapsed := time.Since(delStart)
	fmt.Printf("DeleteImage    : OK %q in %s\n", target, elapsed.Round(time.Millisecond))

	after, err := client.ListImages(ctx)
	if err != nil {
		fail("ListImages after delete: %v", err)
	}
	if len(after) >= len(images) {
		fail("image count did not drop: %d before, %d after", len(images), len(after))
	}
	fmt.Printf("verify         : OK %d images before, %d after\n", len(images), len(after))

	// pkg/remover budgets a single 5m context for the whole batch, so surface how
	// much of it one Windows image consumes.
	fmt.Printf("timing         : %s to delete one image\n", elapsed.Round(time.Second))
}

func unusedImages(images []*v1.Image, containers []*v1.Container) []*v1.Image {
	inUse := map[string]bool{}
	for _, c := range containers {
		if c.Image != nil {
			inUse[c.Image.Image] = true
		}
		inUse[c.ImageRef] = true
	}

	out := make([]*v1.Image, 0, len(images))
	for _, img := range images {
		if !inUse[img.Id] {
			out = append(out, img)
		}
	}
	return out
}

func name(img *v1.Image) string {
	if len(img.RepoTags) > 0 {
		return img.RepoTags[0]
	}
	return img.Id
}

func gb(b uint64) float64 { return float64(b) / (1 << 30) }

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL "+format+"\n", args...)
	os.Exit(1)
}
