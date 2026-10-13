package sandbox

import (
	"time"

	"golang.org/x/sys/unix"
)

func applyRlimits(limit Limit) error {
	limits := []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_CPU, uint64(limit.Time.Truncate(time.Second)/time.Second) + 1},
		{unix.RLIMIT_FSIZE, limit.OutputSize},
		{unix.RLIMIT_STACK, limit.Stack},
		{unix.RLIMIT_NOFILE, 64},
		{unix.RLIMIT_CORE, 0},
	}
	for _, item := range limits {
		if err := unix.Setrlimit(item.resource, &unix.Rlimit{Cur: item.value, Max: item.value}); err != nil {
			return err
		}
	}
	return nil
}
