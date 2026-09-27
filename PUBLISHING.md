# Publishing the Go module

Go has no registry to upload to. A module is published by **pushing a tag to a public repo**;
`proxy.golang.org` and `pkg.go.dev` pick it up on first request.

## Once, to set up

1. Create `github.com/igaurav-dev/pdfcraft-go` as a **public** repo. A private repo cannot be
   fetched by the module proxy, and `go get` will fail with a 404 that looks like a typo.
2. The module path in `go.mod` must match the repo URL exactly. It does — `module
   github.com/igaurav-dev/pdfcraft-go`. If the repo is ever renamed, the module path changes
   and every importer breaks, so pick the name once.

## Every release

```bash
# 1. The contract, first.
node ../pdfcraft/packages/contract/scripts/sync-polyglot.mjs .
git diff --stat contract_gen.go

# 2. Verify. All three must be clean.
gofmt -l .        # must print nothing
go vet ./...
go test ./...

# 3. Tag and push. The tag IS the release.
git tag v1.3.0
git push origin main --tags

# 4. Ask the proxy to fetch it, which also warms pkg.go.dev.
GOPROXY=proxy.golang.org go list -m github.com/igaurav-dev/pdfcraft-go@v1.3.0
```

## The rule that has no undo

**A tag that the module proxy has fetched is immutable.** Deleting and re-pushing the same tag
does not help: the proxy serves its cached copy, and a `go get` on a fresh machine will get the
old code while yours has the new. A mistake in v1.3.0 means tagging v1.3.1.

`go vet ./... && go test ./...` before the tag, every time.

## Versions past v1

If this ever reaches v2, the module path must gain a `/v2` suffix in `go.mod` *and* in every
import path. That is Go's rule, not a convention, and skipping it produces a module nobody can
import at v2.

## Keep in step

The three SDKs share a version line so a reader can tell whether their Go client knows about
the same contract as their TypeScript one. When the contract changes, sync and release all
three, or none.
