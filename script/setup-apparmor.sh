#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
binary="$script_dir/../bin/justcpc-judge-engine"
print_only=0

usage() {
    cat <<'EOF'
Usage: sudo ./script/setup-apparmor.sh [--binary PATH]
       ./script/setup-apparmor.sh [--binary PATH] --print

Permit sandbox namespace creation for this engine's exact executable path.
--print shows the profile without installing it or changing host policy.
No service is started. Global kernel settings are not changed.
EOF
}

while (($#)); do
    case "$1" in
        --binary)
            [[ $# -ge 2 && -n "$2" ]] || { printf 'Missing --binary path.\n' >&2; exit 2; }
            binary="$2"
            shift 2
            ;;
        --print) print_only=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done

if [[ ! -f "$binary" || ! -x "$binary" ]]; then
    printf 'Build the engine first: go build -o bin/justcpc-judge-engine ./src\nMissing executable: %s\n' "$binary" >&2
    exit 1
fi
binary="$(readlink -f -- "$binary")"
# AppArmor attachment names are patterns. Only install an exact literal path.
for character in '*' '?' '[' ']' '{' '}' '"' '\' $'\n' $'\r'; do
    if [[ "$binary" == *"$character"* ]]; then
        printf 'Unsupported AppArmor executable path character; use a path without glob, quote, backslash or newline characters: %s\n' "$binary" >&2
        exit 1
    fi
done
read -r digest _ < <(printf '%s' "$binary" | sha256sum)
profile_name="justcpc-judge-engine-${digest:0:12}"
profile_path="/etc/apparmor.d/$profile_name"

profile() {
    cat <<EOF
abi <abi/4.0>,
include <tunables/global>

# Permit namespaces only for this engine path. The engine's own sandbox
# continues to apply cgroup, namespace, filesystem and seccomp restrictions.
profile $profile_name "$binary" flags=(unconfined) {
  userns,
}
EOF
}

if ((print_only)); then
    profile
    exit 0
fi
if [[ ! -r /sys/module/apparmor/parameters/enabled ]] ||
   [[ "$(< /sys/module/apparmor/parameters/enabled)" != "Y" ]] ||
   [[ ! -r /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]] ||
   [[ "$(< /proc/sys/kernel/apparmor_restrict_unprivileged_userns)" != "1" ]]; then
    printf 'AppArmor user namespace restriction is not enabled; no profile installation is needed.\n'
    exit 0
fi
if ((EUID != 0)); then
    printf 'Host policy installation requires sudo. Run:\n  sudo %q --binary %q\n' "$0" "$binary" >&2
    exit 1
fi
if ! command -v apparmor_parser >/dev/null 2>&1; then
    printf 'apparmor_parser is missing. Install the Ubuntu apparmor package, then retry.\n' >&2
    exit 1
fi

temporary="$(mktemp)"
trap 'rm -f -- "$temporary"' EXIT
profile > "$temporary"
apparmor_parser --skip-kernel-load --skip-cache "$temporary"
install -o root -g root -m 0644 "$temporary" "$profile_path"
apparmor_parser --replace "$profile_path"
printf 'Installed and loaded %s for %s. No service was started.\n' "$profile_path" "$binary"
