# Viant distribution

This fork retains the upstream MIT license and Go module path. Its container
workflow publishes multi-architecture images to `ghcr.io/viant/bigquery-emulator`
after the metadata/server tests pass. Every main-branch build has an immutable
`sha-<40-character-commit>` tag; deployments should additionally pin its digest.
Version tags continue to publish the corresponding semantic-version tags.

## Request metadata and saved job results

Ordinary request admission loads project/dataset metadata and saved job
references. It does not decode every historical job response before reading the
request body. Job lookup, listing and resumable-upload paths explicitly load the
required job data, preserving their response and error semantics.

Run the regression and allocation benchmark with:

```sh
go test ./internal/metadata ./server
go test ./internal/metadata -run TestRequestProject -bench BenchmarkProjectLookupWithHistory -benchmem
```

The benchmark distinguishes request allocation pressure from persistent heap
retention. Saved query jobs/results remain stored intentionally; this patch does
not erase history or increase server read deadlines. It is not proof that all
memory leaks in the server or SQL driver are resolved.
