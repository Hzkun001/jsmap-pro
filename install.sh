#!/bin/sh
set -eu

case "$(uname -s)" in
    Linux|Darwin) ;;
    *) echo "This installer supports Linux and macOS." >&2; exit 1 ;;
esac

source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=${PREFIX:-/usr/local}
case "$prefix" in
    /*) ;;
    *) echo "PREFIX must be an absolute path." >&2; exit 1 ;;
esac
bin_dir=${prefix%/}/bin
case ":$PATH:" in
    *:"$bin_dir":*) ;;
    *) echo "$bin_dir is not in PATH. Choose a PREFIX whose bin directory is already in PATH." >&2; exit 1 ;;
esac

binary=$source_dir/jsmap
if [ -f "$source_dir/go.mod" ] && [ -f "$source_dir/main.go" ]; then
    command -v go >/dev/null 2>&1 || {
        echo "Building from source requires Go 1.24+. Alternatively, download a release archive." >&2
        exit 1
    }
    build_dir=$(mktemp -d "${TMPDIR:-/tmp}/jsmap-install.XXXXXX")
    trap 'rm -rf "$build_dir"' 0
    trap 'exit 130' INT
    trap 'exit 143' TERM
    echo "Building jsmap..."
    (cd "$source_dir" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$build_dir/jsmap" .)
    binary=$build_dir/jsmap
elif [ ! -f "$binary" ]; then
    echo "No source or jsmap binary found beside install.sh." >&2
    exit 1
fi

if [ "$(id -u)" = 0 ] || { [ -d "$bin_dir" ] && [ -w "$bin_dir" ] && { [ ! -e "$bin_dir/jsmap" ] || [ -w "$bin_dir/jsmap" ]; }; }; then
    mkdir -p "$bin_dir"
    install -m 755 "$binary" "$bin_dir/jsmap"
else
    command -v sudo >/dev/null 2>&1 || { echo "Installing to $bin_dir requires write permission or sudo." >&2; exit 1; }
    echo "Installing to $bin_dir (sudo may request your password)..."
    sudo mkdir -p "$bin_dir"
    sudo install -m 755 "$binary" "$bin_dir/jsmap"
fi

echo "Installed: $bin_dir/jsmap"
echo "Run from any directory: jsmap example.com"
