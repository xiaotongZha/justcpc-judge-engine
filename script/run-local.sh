#!/usr/bin/env bash
set -euo pipefail

# Local development needs the same cgroup delegation as the installed service.
# This wrapper only asks systemd for that delegation; it never elevates itself.

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "$script_dir/.." && pwd)"
binary="$project_dir/bin/justcpc-judge-engine"

if (($# > 1)); then
    printf 'Usage: %s [--check]\n' "$0" >&2
    exit 2
fi
if [[ "${1:-}" != "" && "${1:-}" != "--check" && "${1:-}" != "--help" ]]; then
    printf 'Usage: %s [--check]\n' "$0" >&2
    exit 2
fi
if [[ "${1:-}" == "--help" ]]; then
    printf 'Usage: %s [--check]\n--check validates the actual startup environment without starting the HTTP service.\n' "$0"
    exit 0
fi

if [[ ! -x "$binary" ]]; then
    printf 'Local debug binary is missing: %s\n' "$binary" >&2
    printf 'Build it first (as a normal user):\n  cd %s && go build -o bin/justcpc-judge-engine ./src\n' "$project_dir" >&2
    exit 1
fi

cd "$project_dir"

if ((EUID == 0)); then
    printf 'Run local development as your ordinary login user; use sudo only for script/setup-apparmor.sh.\n' >&2
    exit 1
fi
if [[ ! -f /sys/fs/cgroup/cgroup.controllers ]]; then
    printf 'cgroup v2 is required; this host does not expose /sys/fs/cgroup/cgroup.controllers.\n' >&2
    exit 1
fi

if ! command -v systemd-run >/dev/null 2>&1 || ! systemctl --user show-environment >/dev/null 2>&1; then
    printf 'A systemd user session is required for local cgroup delegation.\nRegister the system service first: sudo ./script/register-service.sh\n' >&2
    exit 1
fi
if ! "$binary" --check-namespaces; then
    printf '\nFirst-time host setup on Ubuntu with AppArmor restrictions:\n  sudo %q --binary %q\nThen retry this command. See README for other kernel or container restrictions.\n' "$script_dir/setup-apparmor.sh" "$binary" >&2
    exit 1
fi
exec systemd-run --user --scope -p Delegate=yes -p TasksMax=infinity -- "$binary" "$@"
