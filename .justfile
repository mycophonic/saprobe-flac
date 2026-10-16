# This file is the project's own.
# Add recipes leveraging provided `do` ready-made recipes, or create your own.
# The import must be kept: it mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# The FIRST recipe defined here becomes `just`'s default.
# The shared default covers the language-agnostic linters (limen, just, aqua,
# yaml, shell, dockerfile, commits); the Go linters (golangci-lint, govulncheck,
# go mod tidy, go-licenses) live in their own submodule and are added explicitly.
[doc('Lint everything (shared linters + Go linters)')]
lint: do::lint::default do::lint::go::default

# Shared fixers plus the Go fixers (golangci-lint --fix, go mod tidy).
[doc('Auto-fix what can be fixed (shared fixers + Go fixers)')]
fix: do::fix::default do::fix::go::default

# Unit, race and cover. Benchmarks are a performance measure, not a test:
# `just do perf go bench`.
[doc('Run the Go test suite: unit, race, cover')]
test: do::test::go::unit do::test::go::race do::test::go::cover

# The example decoder is an example program, not a product (limen's
# book/releasing.md), so it lives under examples/ and is built here: the
# shared recipe reads cmd/ alone.
[doc('Build the example decoder under build/')]
build:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p build
    version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
    go build -trimpath -ldflags "-s -w -X main.version=${version}" -o build/decoder ./examples/decoder

# The shared security scans (govulncheck, for a Go module); the security
# workflow runs this recipe.
[doc('Run the security scans')]
security: do::security::default
