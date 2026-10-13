#!/usr/bin/env bash
set -euo pipefail

# Run the judge engine in local debug mode. The sandbox refuses the host cgroup
# v2 root, so the engine runs inside a delegated cgroup subtree.
#
# Preferred path (no root): a systemd user scope with Delegate=yes gives the
# engine its own writable cgroup subtree.
#
# Fallback: when no systemd user session is available the script re-runs itself
# with sudo and creates a manual delegated cgroup.
#
# Usage:
#   go build -o bin/justcpc-judge-engine ./src
#   ./script/run-local.sh

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "$script_dir/.." && pwd)"
binary="$project_dir/bin/justcpc-judge-engine"
cgroup_name="justcpc-engine-local"
cgroup_path="/sys/fs/cgroup/$cgroup_name"

if [[ ! -x "$binary" ]]; then
    printf 'Local debug binary is missing: %s\n' "$binary" >&2
    printf 'Build it first (as a normal user):\n  cd %s && go build -o bin/justcpc-judge-engine ./src\n' "$project_dir" >&2
    exit 1
fi

cd "$project_dir"

# Preferred: systemd user-scope delegation, no root required.
if systemctl --user is-system-running >/dev/null 2>&1; then
    exec systemd-run --user --scope -p Delegate=yes -p TasksMax=infinity -- "$binary"
fi

# Fallback: manual delegated cgroup, re-invoking with root.
if (( EUID != 0 )); then
    printf 'systemd user session is unavailable; re-running with sudo.\n' >&2
    exec sudo "$0"
fi
if [[ ! -d /sys/fs/cgroup ]] || [[ ! -f /sys/fs/cgroup/cgroup.controllers ]]; then
    printf 'cgroup v2 is not available on this host.\n' >&2
    exit 1
fi
mkdir -p "$cgroup_path"
# Remove task cgroups left behind by an earlier run; subtree_control can only be
# changed while the cgroup and its children are empty.
for child in "$cgroup_path"/justcpc-*; do
    [[ -e "$child" ]] && rmdir "$child" 2>/dev/null || true
done
printf '+memory +pids' > "$cgroup_path/cgroup.subtree_control"
printf '%s\n' "$$" > "$cgroup_path/cgroup.procs"
exec "$binary"
