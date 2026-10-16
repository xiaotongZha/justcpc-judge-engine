#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    [[ "$1" == "--help" && $# == 1 ]] && { printf 'Usage: VERSION=0.2.1 ./script/build-release.sh\n'; exit 0; }
    printf 'Usage: VERSION=0.2.1 ./script/build-release.sh\n' >&2; exit 2
fi
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "$script_dir/.." && pwd)"
version="${VERSION:-0.2.1}"
[[ "$version" =~ ^[[:alnum:]][[:alnum:]._-]*$ ]] || { printf 'Invalid VERSION.\n' >&2; exit 2; }
for tool in go tar sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'Missing build tool: %s\n' "$tool" >&2; exit 1; }
done
release_root="$project_dir/release"
mkdir -p "$release_root"
temporary="$(mktemp -d "$release_root/.build-XXXXXX")"
trap 'rm -rf -- "$temporary"' EXIT
name="JustCPC-Judge-$version-linux-amd64"
package="$temporary/$name"
mkdir -p "$package/bin" "$package/script"
(cd "$project_dir"; CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$package/bin/justcpc-judge-engine" ./src)
cp "$script_dir/run-local.sh" "$script_dir/setup-apparmor.sh" "$script_dir/register-service.sh" "$package/script/"
cp "$project_dir/config.example.json" "$package/"
cp "$project_dir/DEPLOY.md" "$package/README.md"
cat > "$package/start.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname -- "$0")"
exec ./script/run-local.sh "$@"
EOF
cat > "$package/install-service.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname -- "$0")"
exec ./script/register-service.sh "$@"
EOF
chmod +x "$package/"*.sh "$package/script/"*.sh
tar -C "$temporary" -czf "$temporary/$name.tar.gz" "$name"
mv -f "$temporary/$name.tar.gz" "$release_root/"
(cd "$release_root"; sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
printf 'Release: %s/%s.tar.gz\n' "$release_root" "$name"
