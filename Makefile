NAME := saprobe-flac
ALLOWED_LICENSES := Apache-2.0,BSD-2-Clause,BSD-3-Clause,MIT
LICENSE_IGNORES := --ignore gotest.tools

include hack/common.mk

# CGO is needed for tests only (CoreAudio reference decoder benchmarks).
# The decoder itself is pure Go — builds must remain CGO_ENABLED=0.
test-unit test-unit-bench test-unit-profile test-unit-cover: export CGO_ENABLED = 1
