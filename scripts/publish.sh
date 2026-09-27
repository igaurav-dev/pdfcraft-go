#!/usr/bin/env bash
# scripts/publish.sh
#
# Go has no registry to upload to. A module is published by pushing a TAG to a
# PUBLIC repo; proxy.golang.org and pkg.go.dev fetch it on first request. So
# this script's job is different from the npm and PyPI ones: it verifies the
# things that make a tag fetchable, then tags and pushes.
#
#   ./scripts/publish.sh --dry-run          # every check, stops before tagging
#   ./scripts/publish.sh                    # checks, then asks, then tags+pushes
#   ./scripts/publish.sh --yes              # no prompt, for CI
#
# THE RULE WITH NO UNDO: once the module proxy has fetched a tag it caches it
# forever. Deleting and re-pushing the same tag serves the OLD code to everyone
# except you, which is the most confusing failure in Go packaging. A mistake in
# v1.3.0 means v1.3.1.
set -euo pipefail

DRY_RUN=0
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --yes | -y) ASSUME_YES=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

cd "$(dirname "${BASH_SOURCE[0]}")/.."
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

step() { printf '\n\033[1m%s\033[0m\n' "$1"; }
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
skip() { printf '  \033[33mskip\033[0m %s\n' "$1"; }
die()  { printf '  \033[31mfail\033[0m %s\n' "$1" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die 'go is not installed'
MODULE="$(go list -m)"
VERSION="v$(grep -oE 'Version = "[^"]+"' client.go | head -1 | cut -d'"' -f2)"
printf '\033[1m%s %s\033[0m\n' "$MODULE" "$VERSION"

step '1 · Repository state'
git rev-parse --git-dir >/dev/null 2>&1 || die 'not a git checkout — a Go module IS its repo, so there is nothing to publish'
DIRTY="$(git status --porcelain -- . 2>/dev/null)"
if [ -n "$DIRTY" ]; then
  printf '  \033[31mfail\033[0m uncommitted changes in %s:\n%s\n' \
    "$PWD" "$(printf '%s\n' "$DIRTY" | sed 's/^/         /')" >&2
  exit 1
fi
ok "clean at $(git rev-parse --short HEAD)"

step '2 · Module path matches the remote'
# If these disagree, `go get` fetches the repo, reads a different module path in
# go.mod, and fails with an error that reads like the user made a typo.
REMOTE="$(git remote get-url origin 2>/dev/null || true)"
[ -n "$REMOTE" ] || die 'no origin remote — push the repo to GitHub first'
EXPECTED="$(printf '%s' "$REMOTE" | sed -e 's#^git@github.com:#github.com/#' -e 's#^https://##' -e 's#\.git$##')"
[ "$MODULE" = "$EXPECTED" ] || die "go.mod says
         module $MODULE
       but origin is
         $EXPECTED
       These must be identical or nobody can import this."
ok "$MODULE"

step '3 · The repo must be PUBLIC'
# The single most common reason a freshly tagged Go module cannot be installed.
# proxy.golang.org is an anonymous client; a private repo is a 404 to it, and
# the error the user sees looks like a misspelled path.
if command -v curl >/dev/null 2>&1; then
  CODE="$(curl -s -o /dev/null -w '%{http_code}' -L --max-time 15 "https://$EXPECTED" || echo 000)"
  case "$CODE" in
    200) ok "public — https://$EXPECTED" ;;
    404) die "https://$EXPECTED 404s for a logged-out visitor.
       Either it does not exist yet or it is private. The module proxy is
       anonymous, so a private repo can never be fetched — make it public." ;;
    000) skip 'no network — could not verify the repo is public' ;;
    *)   skip "answered HTTP $CODE, which proves nothing — check by hand" ;;
  esac
else
  skip 'curl not installed'
fi

step '4 · The tag must not already exist'
if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
  die "$VERSION already exists locally. The proxy caches tags permanently, so
       re-pointing one is worse than useless — bump the version in client.go."
fi
if git ls-remote --exit-code --tags origin "$VERSION" >/dev/null 2>&1; then
  die "$VERSION already exists on origin. Bump the version in client.go."
fi
ok "$VERSION is unclaimed"

step '5 · Major version suffix'
# Go's rule, not a convention: from v2 the module path must end in /vN, in
# go.mod AND in every import path. Skipping it produces a module nobody can
# import at that version.
MAJOR="$(printf '%s' "$VERSION" | cut -d. -f1 | tr -d v)"
if [ "$MAJOR" -ge 2 ]; then
  case "$MODULE" in
    */v"$MAJOR") ok "module path carries /v$MAJOR" ;;
    *) die "publishing $VERSION requires the module path to end in /v$MAJOR.
       Change go.mod to '$MODULE/v$MAJOR' and update every import." ;;
  esac
else
  ok 'v1 — no path suffix needed'
fi

step '6 · Contract freshness'
SYNC=../pdfcraft/packages/contract/scripts/sync-polyglot.mjs
if [ -f "$SYNC" ] && command -v node >/dev/null 2>&1; then
  BEFORE="$(cat contract_gen.go)"
  node "$SYNC" . >/dev/null
  if [ "$BEFORE" != "$(cat contract_gen.go)" ]; then
    die 'the generated contract was stale and has just been regenerated.
       Review the diff, bump the version, commit, then run this again.'
  fi
  ok 'generated contract matches the API'
else
  skip 'monorepo not beside this checkout — could not verify the contract'
fi

step '7 · Format, vet, test'
FMT="$(gofmt -l .)"
[ -z "$FMT" ] || die "gofmt would change these files:
$(printf '%s\n' "$FMT" | sed 's/^/         /')
       Run: gofmt -w ."
ok 'gofmt clean'
go vet ./... || die 'go vet failed'
ok 'go vet clean'
go test ./... >/dev/null || die 'tests failed — run "go test ./..." to see why'
ok 'tests pass'

step '8 · Build as a consumer would'
# A fresh module that imports this one by path, with a replace pointing here.
# Catches an exported name that does not compile from outside the package.
mkdir -p "$WORK/consumer"
cat >"$WORK/consumer/go.mod" <<EOF
module consumer

go 1.22

require $MODULE v0.0.0

replace $MODULE => $PWD
EOF
cat >"$WORK/consumer/main.go" <<EOF
package main

import (
	"context"
	"errors"
	"fmt"

	"$MODULE"
)

func main() {
	client, err := pdfcraft.New("sk_live_x", pdfcraft.WithBaseURL("http://localhost"))
	if err != nil {
		panic(err)
	}
	_, err = client.Render(context.Background(), pdfcraft.RenderRequest{
		HTML:    "<h1>hi</h1>",
		Options: &pdfcraft.RenderOptions{Format: "A4"},
	})
	var apiErr *pdfcraft.Error
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.Code, apiErr.Retryable)
	}
	fmt.Println(pdfcraft.Version, pdfcraft.ContractHash[:8], len(pdfcraft.ErrorCodes))
}
EOF
(cd "$WORK/consumer" && go build ./... ) || die 'a fresh consumer module cannot build against this package'
ok 'an external module compiles against the public API'

step '9 · Tag and push'
if [ "$DRY_RUN" -eq 1 ]; then
  printf '  every check passed. Re-run without --dry-run to tag and push %s.\n' "$VERSION"
  exit 0
fi
if [ "$ASSUME_YES" -eq 0 ]; then
  printf '  tag %s and push? The proxy caches tags permanently. [y/N] ' "$VERSION"
  read -r answer </dev/tty
  case "$answer" in
    y | Y | yes) ;;
    *) printf '  nothing tagged.\n'; exit 0 ;;
  esac
fi
git tag -a "$VERSION" -m "$MODULE $VERSION"
git push origin HEAD --tags
ok "tagged and pushed $VERSION"

step '10 · Warm the proxy'
# Also what makes pkg.go.dev build the documentation page.
if GOPROXY=proxy.golang.org go list -m "$MODULE@$VERSION" >/dev/null 2>&1; then
  ok "proxy has it — https://pkg.go.dev/$MODULE@$VERSION"
else
  skip 'the proxy has not picked it up yet; it usually takes a minute'
  printf '    retry: GOPROXY=proxy.golang.org go list -m %s@%s\n' "$MODULE" "$VERSION"
fi
