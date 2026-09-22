//go:build !linux

package sys

func CgroupCPUCapacity() (float64, bool) {
	return 0, false
}

func CgroupMemoryUsage() (current uint64, limit uint64, ok bool) {
	return 0, 0, false
}
