#!/bin/sh
# Apply the reviewed wasm32 buffer fix to a private copy of the pinned module.
# Never modify the shared Go module cache or the repository's module manifest.
set -eu
export GOFLAGS= GOWORK=off
cd "$(dirname "$0")/.."
module=github.com/goccy/go-googlesql
version=v0.4.0
work=.patched-googlesql
test "$(go list -m -f '{{.Version}}' "$module")" = "$version"
go mod download "$module@$version"
source=$(go list -m -f '{{.Dir}}' "$module")
test -n "$source"
mkdir -p "$work"
rm -rf "$work/source"
cp -R "$source" "$work/source"
chmod -R u+w "$work/source"
git apply --check --directory="$work/source" scripts/go-googlesql-wasm32.patch
git apply --directory="$work/source" scripts/go-googlesql-wasm32.patch
test -s "$work/source/wasm_memory.go"
test -s "$work/source/wasm_memory_large_test.go"
cp go.mod "$work/emulator.mod"
cp go.sum "$work/emulator.sum"
go mod edit -modfile="$work/emulator.mod" -replace="$module=./$work/source"
printf 'Prepared %s %s with scripts/go-googlesql-wasm32.patch\n' "$module" "$version"
