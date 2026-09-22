//go:build linux

package sys

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCgroupFile(t *testing.T, root, name, value string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCgroupV2CPUPercentUsesQuota(t *testing.T) {
	root := t.TempDir()
	writeCgroupFile(t, root, "cpu.max", "200000 100000\n")
	writeCgroupFile(t, root, "cpu.stat", "usage_usec 1000000\nuser_usec 700000\n")

	capacity, limited, err := readCPUQuota(root)
	if err != nil || !limited || capacity != 2 {
		t.Fatalf("capacity = %v, limited = %v, err = %v", capacity, limited, err)
	}

	var sampler cgroupCPUSampler
	t0 := time.Unix(100, 0)
	if percent, ok, err := cgroupCPUPercentAt(root, t0, &sampler); err != nil || !ok || percent != 0 {
		t.Fatalf("first sample = %v, ok = %v, err = %v", percent, ok, err)
	}
	writeCgroupFile(t, root, "cpu.stat", "usage_usec 1500000\n")
	percent, ok, err := cgroupCPUPercentAt(root, t0.Add(time.Second), &sampler)
	if err != nil || !ok || math.Abs(percent-25) > 0.001 {
		t.Fatalf("second sample = %v, ok = %v, err = %v; want 25", percent, ok, err)
	}
}

func TestCgroupV2MemoryUsage(t *testing.T) {
	root := t.TempDir()
	writeCgroupFile(t, root, "memory.current", "402653184\n")
	writeCgroupFile(t, root, "memory.max", "1610612736\n")

	oldRoot := cgroupFSRoot
	cgroupFSRoot = root
	t.Cleanup(func() { cgroupFSRoot = oldRoot })

	current, limit, ok := CgroupMemoryUsage()
	if !ok || current != 402653184 || limit != 1610612736 {
		t.Fatalf("memory = (%d, %d, %v)", current, limit, ok)
	}
}

func TestUnlimitedCgroupFallsBackToHost(t *testing.T) {
	root := t.TempDir()
	writeCgroupFile(t, root, "cpu.max", "max 100000\n")
	writeCgroupFile(t, root, "memory.current", "1024\n")
	writeCgroupFile(t, root, "memory.max", "max\n")

	if capacity, limited, err := readCPUQuota(root); err != nil || limited || capacity != 0 {
		t.Fatalf("capacity = %v, limited = %v, err = %v", capacity, limited, err)
	}

	oldRoot := cgroupFSRoot
	cgroupFSRoot = root
	t.Cleanup(func() { cgroupFSRoot = oldRoot })
	if _, _, ok := CgroupMemoryUsage(); ok {
		t.Fatal("unlimited memory cgroup must fall back to host metrics")
	}
}

func TestCgroupV1Layout(t *testing.T) {
	root := t.TempDir()
	writeCgroupFile(t, root, "cpu/cpu.cfs_quota_us", "50000\n")
	writeCgroupFile(t, root, "cpu/cpu.cfs_period_us", "100000\n")
	writeCgroupFile(t, root, "cpuacct/cpuacct.usage", "1000000000\n")
	writeCgroupFile(t, root, "memory/memory.usage_in_bytes", "268435456\n")
	writeCgroupFile(t, root, "memory/memory.limit_in_bytes", "536870912\n")

	capacity, limited, err := readCPUQuota(root)
	if err != nil || !limited || capacity != 0.5 {
		t.Fatalf("capacity = %v, limited = %v, err = %v", capacity, limited, err)
	}

	oldRoot := cgroupFSRoot
	cgroupFSRoot = root
	t.Cleanup(func() { cgroupFSRoot = oldRoot })
	current, limit, ok := CgroupMemoryUsage()
	if !ok || current != 268435456 || limit != 536870912 {
		t.Fatalf("memory = (%d, %d, %v)", current, limit, ok)
	}
}
