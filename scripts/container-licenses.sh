#!/bin/sh
# Preserve license/NOTICE texts for the exact linked Go dependency graph.
set -eu
out=${1:?license output directory is required}
tool=${GO_LICENSES_BIN:-/go/bin/go-licenses}
mkdir -p "$out"
# These two complete license files are handled verbatim below. The classifier
# cannot identify Arrow's multi-license bundle or SQLite WASM's MIT-0 text.
# Exact version/content checks make an unnoticed dependency change fail closed.
"$tool" save ./cmd/bigquery-emulator --save_path="$out/third-party" \
  --ignore=github.com/apache/arrow/go/v15,github.com/ncruces/go-sqlite3-wasm/v2
copy_reviewed_license() {
  module=$1 version=$2 filename=$3 expected=$4
  test "$(go list -m -f '{{.Version}}' "$module")" = "$version"
  module_dir=$(go list -m -f '{{.Dir}}' "$module")
  printf '%s  %s\n' "$expected" "$module_dir/$filename" | sha256sum -c -
  mkdir -p "$out/third-party/$module"
  cp "$module_dir/$filename" "$out/third-party/$module/$filename"
}
copy_reviewed_license github.com/apache/arrow/go/v15 v15.0.2 LICENSE.txt bd939b3a0ca7ffedd7444240915f4f297f09739fa9d07bac8c85ec4ea8c94c35
copy_reviewed_license github.com/ncruces/go-sqlite3-wasm/v2 v2.2.35301 LICENSE 13219037ddf63dbbcf174bf59525d602df7a2e30083f63be566715c858fcb19e
cp licenses/apache-arrow-v15/NOTICE.txt "$out/third-party/github.com/apache/arrow/go/v15/NOTICE.txt"
cp licenses/apache-arrow-v15/SOURCE.txt "$out/third-party/github.com/apache/arrow/go/v15/SOURCE.txt"
sqlite_dir=$(go list -m -f '{{.Dir}}' github.com/goccy/googlesqlite)
mkdir -p "$out/third-party/github.com/goccy/googlesqlite/docs/third_party/googlesql-docs"
cp "$sqlite_dir/docs/third_party/googlesql-docs/LICENSE" "$out/third-party/github.com/goccy/googlesqlite/docs/third_party/googlesql-docs/LICENSE"
cp LICENSE "$out/LICENSE"
mkdir -p "$out/go"
cp "$(go env GOROOT)/LICENSE" "$out/go/LICENSE"
go list -m -f '{{.Path}} {{.Version}}{{with .Replace}} => {{.Path}} {{.Version}}{{end}}' all > "$out/dependency-versions.txt"
cat > "$out/README.txt" <<'TEXT'
LICENSE is the emulator's original upstream MIT license and copyright notice.
go/LICENSE is the Go toolchain/standard-library license.
third-party/ contains dependency license and adjacent NOTICE files, collected
with google/go-licenses v2.0.1 for the target binary package.
Arrow v15's complete multi-license LICENSE.txt and SQLite WASM v2's MIT-0 LICENSE
are copied verbatim with pinned version and SHA-256 checks because the automated
classifier cannot identify those texts. Their dependencies remain scanned.
dependency-versions.txt records the resolved module graph and published remote
replacements. Debian base-image notices remain in /usr/share/doc and
/usr/share/common-licenses.
TEXT
