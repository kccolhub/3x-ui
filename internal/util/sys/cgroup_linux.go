//go:build linux

package sys

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cgroupUnlimited = uint64(1) << 62

var cgroupFSRoot = "/sys/fs/cgroup"

type cgroupCPUSampler struct {
	sync.Mutex
	initialized bool
	usageNS     uint64
	at          time.Time
}

var containerCPUSampler cgroupCPUSampler

func readCgroupUint(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func readCPUQuota(root string) (float64, bool, error) {
	if b, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) != 2 {
			return 0, true, fmt.Errorf("invalid cpu.max")
		}
		if fields[0] == "max" {
			return 0, false, nil
		}
		quota, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, true, err
		}
		period, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || quota <= 0 || period <= 0 {
			return 0, true, fmt.Errorf("invalid cpu.max quota")
		}
		return quota / period, true, nil
	}

	quota, err := readCgroupUint(filepath.Join(root, "cpu", "cpu.cfs_quota_us"))
	if err != nil {
		return 0, false, nil
	}
	period, err := readCgroupUint(filepath.Join(root, "cpu", "cpu.cfs_period_us"))
	if err != nil || quota == 0 || period == 0 || quota >= cgroupUnlimited {
		return 0, false, nil
	}
	return float64(quota) / float64(period), true, nil
}

func readCPUUsageNS(root string) (uint64, bool, error) {
	if b, err := os.ReadFile(filepath.Join(root, "cpu.stat")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "usage_usec" {
				usage, err := strconv.ParseUint(fields[1], 10, 64)
				return usage * 1000, true, err
			}
		}
		return 0, true, fmt.Errorf("cpu.stat has no usage_usec")
	}

	usage, err := readCgroupUint(filepath.Join(root, "cpuacct", "cpuacct.usage"))
	if err != nil {
		return 0, false, nil
	}
	return usage, true, nil
}

// CgroupCPUCapacity returns the finite CPU quota visible to this container.
// A false result means the process is not CPU-limited and callers should use
// host CPU information.
func CgroupCPUCapacity() (float64, bool) {
	capacity, limited, err := readCPUQuota(cgroupFSRoot)
	return capacity, limited && err == nil && capacity > 0
}

func cgroupCPUPercentAt(root string, now time.Time, sampler *cgroupCPUSampler) (float64, bool, error) {
	capacity, limited, err := readCPUQuota(root)
	if err != nil || !limited {
		return 0, limited, err
	}
	usageNS, found, err := readCPUUsageNS(root)
	if err != nil || !found {
		return 0, found, err
	}

	sampler.Lock()
	defer sampler.Unlock()
	if !sampler.initialized || usageNS < sampler.usageNS || !now.After(sampler.at) {
		sampler.initialized = true
		sampler.usageNS = usageNS
		sampler.at = now
		return 0, true, nil
	}

	usageDelta := usageNS - sampler.usageNS
	elapsed := now.Sub(sampler.at)
	sampler.usageNS = usageNS
	sampler.at = now
	percent := float64(usageDelta) / float64(elapsed) / capacity * 100
	return math.Min(100, math.Max(0, percent)), true, nil
}

func cgroupCPUPercent() (float64, bool, error) {
	return cgroupCPUPercentAt(cgroupFSRoot, time.Now(), &containerCPUSampler)
}

// CgroupMemoryUsage reports memory charged to this container and its finite
// limit. It supports unified cgroup v2 and the conventional cgroup v1 layout.
func CgroupMemoryUsage() (current uint64, limit uint64, ok bool) {
	current, currentErr := readCgroupUint(filepath.Join(cgroupFSRoot, "memory.current"))
	limit, limitErr := readCgroupUint(filepath.Join(cgroupFSRoot, "memory.max"))
	if currentErr == nil && limitErr == nil && limit > 0 && limit < cgroupUnlimited {
		return current, limit, true
	}

	current, currentErr = readCgroupUint(filepath.Join(cgroupFSRoot, "memory", "memory.usage_in_bytes"))
	limit, limitErr = readCgroupUint(filepath.Join(cgroupFSRoot, "memory", "memory.limit_in_bytes"))
	if currentErr == nil && limitErr == nil && limit > 0 && limit < cgroupUnlimited {
		return current, limit, true
	}
	return 0, 0, false
}
