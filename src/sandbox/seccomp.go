package sandbox

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	auditArchX86_64       = 0xc000003e
	seccompSetModeFilter  = 1
	seccompRetKillProcess = 0x80000000
	seccompRetAllow       = 0x7fff0000
)

// applySeccomp uses an allow list. The compiler profile permits process
// creation. Java needs socket syscalls for local JVM communication; the
// separate network namespace, rather than seccomp, keeps tasks off the host
// network. Neither profile permits ptrace.
func applySeccomp(profile Profile) error {
	syscalls := runtimeSyscalls
	if profile == ProfileCompile {
		syscalls = append(append([]uint32{}, runtimeSyscalls...), compileOnlySyscalls...)
	}
	filters := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, K: auditArchX86_64},
		{Code: unix.BPF_RET | unix.BPF_K, K: seccompRetKillProcess},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
	}
	for _, number := range syscalls {
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: number},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: seccompRetAllow},
		)
	}
	filters = append(filters, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: seccompRetKillProcess})
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, seccompSetModeFilter, 0, uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return fmt.Errorf("load seccomp filter: %w", errno)
	}
	return nil
}

// Linux amd64 syscall numbers. The delivered RootFS is amd64, so accepting a
// different architecture is a deployment error rather than silently running a
// mismatched policy.
var runtimeSyscalls = []uint32{
	0, 1, 2, 3, 4, 5, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 19, 20, 21, 24, 25, 28,
	32, 33, 35, 39, 41, 51, 53, 54, 56, 59, 60, 63, 72, 73, 77, 79, 80, 81, 83, 87, 89, 97, 98, 99, 102, 104, 107,
	108, 110, 111, 131, 157, 158, 186, 202, 204, 217, 218, 228, 229, 231, 234, 257, 262,
	273, 281, 293, 302, 318, 324, 332, 334,
}

var compileOnlySyscalls = []uint32{
	2, 4, 6, 18, 22, 52, 56, 57, 58, 59, 61, 62, 80, 82, 83, 84, 87, 88, 90,
	95, 98, 99, 111, 133, 137, 217,
	258, 259, 260, 263, 264, 266, 267, 268, 269, 270, 271, 272, 276, 280,
}
