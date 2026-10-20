#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    [[ "$1" == "--help" && $# == 1 ]] && { printf 'Usage: VERSION=1.0.1 ./script/build-release.sh\n'; exit 0; }
    printf 'Usage: VERSION=1.0.1 ./script/build-release.sh\n' >&2; exit 2
fi
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "$script_dir/.." && pwd)"
version="${VERSION:-1.0.1}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { printf 'Invalid VERSION.\n' >&2; exit 2; }
for tool in go tar sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'Missing build tool: %s\n' "$tool" >&2; exit 1; }
done
release_root="$project_dir/build/release"
mkdir -p "$release_root"
temporary="$(mktemp -d "$release_root/.build-XXXXXX")"
trap 'rm -rf -- "$temporary"' EXIT
name="justcpc-judge-engine-$version-linux-amd64"
package="$temporary/$name"
mkdir -p "$package/bin" "$package/script"
(cd "$project_dir"; CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$package/bin/justcpc-judge-engine" ./src)
cp "$script_dir/setup-apparmor.sh" "$package/script/"
cp "$project_dir/"{install,update,uninstall}.sh "$package/"
cp "$project_dir/README.md" "$package/README.md"
cp "$script_dir/register-service.sh" "$package/script/"
cp "$project_dir/config/config.example.json" "$package/config.json"
printf '%s\n' "$version" > "$package/.version"
chmod 600 "$package/config.json"
chmod +x "$package/script/"*.sh
chmod +x "$package/"*.sh "$package/script/"*.sh
tar -C "$temporary" -czf "$temporary/$name.tar.gz" "$name"
mv -f "$temporary/$name.tar.gz" "$release_root/"
(cd "$release_root"; sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
printf 'Release: %s/%s.tar.gz\n' "$release_root" "$name"
