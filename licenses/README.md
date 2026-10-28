# Container license material

The image retains the emulator's upstream MIT license and the Go toolchain
license in `/usr/share/licenses/bigquery-emulator/`. It also includes dependency
license texts and adjacent NOTICE files collected for the actual target package
with pinned `google/go-licenses/v2 v2.0.1`.

The classifier has two reviewed exceptions. Their full files are copied verbatim
and guarded by exact module version plus SHA-256 checks:

- Apache Arrow Go v15.0.2 has a composite LICENSE.txt containing its third-party
  license material. Its repository-root NOTICE.txt is supplied here because that
  file is absent from the Go module archive; SOURCE.txt identifies the exact
  upstream commit.
- SQLite WASM v2.2.35301 uses the MIT No Attribution (MIT-0) text, which this
  classifier does not identify.

Ignoring those two package prefixes in the classifier does not skip their
transitive dependencies. New unknown licenses still fail the build. The build
also keeps the SQL driver's NOTICE and the Apache license it references for its
GoogleSQL documentation snapshot. Existing copyright statements are unchanged.
This inventory covers the linked Go package and the license material supplied by
its modules, including embedded runtime bundles; it is not a claim that the
primary MIT license replaces any dependency's separate terms.
