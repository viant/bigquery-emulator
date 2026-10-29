# GoogleSQL wasm32 buffer correction

`go-googlesql-wasm32.patch` applies to `github.com/goccy/go-googlesql v0.4.0`.
It treats WASM addresses as unsigned 32-bit offsets and validates buffer bounds
before slicing Go memory. Addresses above 2 GiB otherwise become negative Go
slice offsets. It also checks request allocation failure and response lengths.

The patch includes neutral boundary tests and an opt-in real allocation test.
The original module license remains in the copied source and image license
inventory. No application schema or fixtures are included.

Docker and `make emulator/build` prepare a private module copy automatically.
For direct Go testing from the repository root:

```sh
sh scripts/prepare-googlesql.sh
export GOFLAGS="-modfile=$PWD/.patched-googlesql/emulator.mod"
go test -p=1 ./internal/metadata ./server
```

To exercise real request and response buffers above 2 GiB:

```sh
cd .patched-googlesql/source
GOFLAGS= GOOGLESQL_LARGE_MEMORY_TEST=1 GOMEMLIMIT=4GiB go test -count=1 -run TestInvokeAboveSignedWasmAddressBoundary .
```

Preparation checks the pinned module version and patch applicability, leaves
the shared module cache and tracked go.mod/go.sum unchanged, and regenerates
only `.patched-googlesql/`. Remove this build patch once a published upstream
module contains the correction and the dependency is upgraded.
