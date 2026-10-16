package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const namespaceProbeArgument = "--justcpc-namespace-probe"

func init() {
	if len(os.Args) == 2 && os.Args[1] == namespaceProbeArgument {
		// The parent creates the namespaces before exec. No mounts, contestant
		// code, cgroups or HTTP listener are needed to check this permission.
		os.Exit(0)
	}
}

func namespaceAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWUSER | unix.CLONE_NEWNS | unix.CLONE_NEWPID | unix.CLONE_NEWNET | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC | unix.CLONE_NEWCGROUP,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
		Pdeathsig:                  syscall.SIGKILL,
	}
}

// CheckNamespaces runs the current executable with the same namespace flags
// and identity mappings as a real sandbox. Testing a different utility (such
// as unshare) would not check the AppArmor policy attached to this executable.
func CheckNamespaces() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, namespaceProbeArgument)
	command.SysProcAttr = namespaceAttributes()
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("create sandbox namespaces with %s: %w (%s)", executable, err, output)
	}
	return nil
}
