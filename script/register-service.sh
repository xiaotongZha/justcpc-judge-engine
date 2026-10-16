#!/usr/bin/env bash
set -euo pipefail

service_name="justcpc-judge-engine"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "$script_dir/.." && pwd)"
release_dir="$project_dir"
run_user="${SUDO_USER:-}"
start_service=1

usage() {
    cat <<'EOF'
Usage: sudo ./script/register-service.sh [options]

Options:
  --release-dir PATH  Directory containing bin/ and config.json.
                      Defaults to the project directory.
  --run-user USER     Linux account that runs the engine. Defaults to the
                      account that invoked sudo.
  --no-start           Register and enable the service without starting it.
  -h, --help           Show this help.
EOF
}

while (($#)); do
    case "$1" in
        --release-dir)
            [[ $# -ge 2 && -n "$2" ]] || { printf 'Missing --release-dir value.\n' >&2; exit 2; }
            release_dir="$2"
            shift 2
            ;;
        --run-user)
            [[ $# -ge 2 && -n "$2" ]] || { printf 'Missing --run-user value.\n' >&2; exit 2; }
            run_user="$2"
            shift 2
            ;;
        --no-start)
            start_service=0
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf 'Unknown option: %s\n' "$1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if ((EUID != 0)); then
    printf 'Run this registration script with sudo.\n' >&2
    exit 1
fi
if [[ -z "$run_user" ]]; then
    printf 'Cannot determine the deployment account. Use --run-user USER when invoking through su.\n' >&2
    exit 1
fi
if ! id "$run_user" >/dev/null 2>&1; then
    printf 'Deployment account does not exist: %s\n' "$run_user" >&2
    exit 1
fi
if [[ ! -d /run/systemd/system ]] || ! command -v systemctl >/dev/null 2>&1; then
    printf 'This host is not running systemd; service registration is unavailable.\n' >&2
    exit 1
fi
if [[ ! -f /sys/fs/cgroup/cgroup.controllers ]]; then
    printf 'This host must use cgroup v2.\n' >&2
    exit 1
fi

release_dir="$(cd -- "$release_dir" && pwd)"
binary="$release_dir/bin/$service_name"
if [[ ! -x "$binary" ]]; then
    printf 'Judge engine binary is missing or not executable: %s\n' "$binary" >&2
    exit 1
fi
if [[ "$release_dir" =~ [[:space:]] ]] || [[ "$binary" =~ [[:space:]] ]]; then
    printf 'The release path must not contain whitespace for the generated systemd unit.\n' >&2
    exit 1
fi

run_group="$(id -gn "$run_user")"
# System-service execution uses the same host namespace policy as local runs.
# Install it before enabling or starting the unit; --no-start stays non-starting.
"$script_dir/setup-apparmor.sh" --binary "$binary"
unit_path="/etc/systemd/system/$service_name.service"
unit_tmp="$(mktemp)"
trap 'rm -f "$unit_tmp"' EXIT

cat >"$unit_tmp" <<EOF
[Unit]
Description=JustCPC Judge Engine
After=network.target

[Service]
Type=simple
User=$run_user
Group=$run_group
WorkingDirectory=$release_dir
ExecStart=$binary
Restart=on-failure
RestartSec=2
Delegate=yes
TasksMax=infinity
KillMode=control-group

[Install]
WantedBy=multi-user.target
EOF

install -o root -g root -m 0644 "$unit_tmp" "$unit_path"
systemctl daemon-reload
systemctl enable "$service_name.service" >/dev/null

if ((start_service)); then
    systemctl restart "$service_name.service"
    if ! systemctl is-active --quiet "$service_name.service"; then
        printf 'The service was registered but did not remain active. Inspect: journalctl -u %s.service\n' "$service_name" >&2
        exit 1
    fi
fi

printf 'Registered %s.service for account %s using release %s\n' "$service_name" "$run_user" "$release_dir"
