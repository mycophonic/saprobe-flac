# saprobe-flac: 360 Audit

Date: 2026-02-03

## Scope

Every file in the repository was reviewed:

**Library code:** `doc.go`, `decode.go` (275 LOC), `encode.go` (157 LOC), `format.go` (44 LOC)
**CLI:** `cmd/flac-example-decoder/main.go` (171 LOC)
**Version:** `version/version.go` (32 LOC)
**Tests:** `tests/conformance_test.go` (376 LOC), `tests/synthetic_test.go` (312 LOC), `tests/benchmark_test.go` (387 LOC), `tests/helpers_test.go` (175 LOC)
**CI:** `.github/workflows/ci.yml`, `.github/workflows/go-latest.yml`
**Linting:** `.golangci.yml` (296 lines, 35+ linters)
**Build:** `Makefile`, `hack/common.mk`, `hack/bench.sh`
**Config:** `go.mod`, `.gitattributes`, `.yamllint`, `.gitignore`, `hack/headers/`, `hack/allowed_signers`
**Docs:** `README.md`, `docs/QA.md`, `docs/OPTIM.md`, `docs/research/ENCODERS.md`, `docs/research/DECODERS.md`

Total: ~500 LOC library, ~1250 LOC tests, ~170 LOC CLI.

## 1. Architecture

Thin adapter over the [mycophonic/flac](https://github.com/mycophonic/flac) fork (itself forked from [mewkiz/flac](https://github.com/mewkiz/flac)).

**Decoder** wraps `goflac.Stream`:
- `NewDecoder(io.ReadSeeker)` opens stream, validates bit depth, returns streaming decoder.
- `Decoder.Read([]byte)` implements `io.Reader`: parses frames on demand, interleaves subframe samples into the caller's buffer.
- `Decode()` convenience function: `NewDecoder` + `io.ReadAll`.
- Buffer reuse: `d.buf` grows but never shrinks. Zero per-frame allocation in steady state.

**Encoder** wraps `goflac.Encoder`:
- `Encode(io.Writer, []byte, PCMFormat)` one-shot: deinterleaves PCM, builds frames, writes via library encoder.
- No streaming encoder API (asymmetric with decoder).

**Core value-add:**
- `interleave()`: converts per-channel `[]int32` subframes to interleaved little-endian PCM bytes. Stereo fast paths use packed writes (`PutUint16`/`PutUint32`/`PutUint64`) with bounds-check elimination hints. Covers all 7 bit depths (4/8/12/16/20/24/32).
- `deinterleave()`: inverse operation for encoding.
- `PCMFormat` + `BitDepth` type contract with `BytesPerSample()` method.

**Single runtime dependency:** `github.com/mewkiz/flac` via replace directive to mycophonic fork.
Test-only dependency: `github.com/mycophonic/agar` (ffmpeg/CoreAudio wrappers, test helpers).

## 2. Code quality

### Strengths

**Zero-allocation decode path.** Memory profile confirms `Frame.Parse` no longer appears as an allocation source. The only decode allocation is the output buffer in `Decode()` (via `io.ReadAll`). The streaming `Decoder.Read` path allocates nothing in steady state — `d.buf` is grow-only, reused across frames.

**Bounds-check elimination.** Every interleave fast path uses BCE hints:
- Upfront assertion: `_ = dst[blockSize*N-1]` before the loop.
- Reslicing to exact capacity: `left := subframes[0].Samples[:blockSize:blockSize]`.
- Range iteration (compiler knows bounds).

**Stereo packed writes.** For 2-channel audio (the overwhelmingly common case), both samples are packed into a single store instruction per iteration:
- 8-bit: `PutUint16` (2 samples in 16 bits)
- 16-bit: `PutUint32` (2 samples in 32 bits)
- 24-bit: `PutUint32` + 2 byte stores (6 bytes per sample pair)
- 32-bit: `PutUint64` (2 samples in 64 bits)

**Error handling.** Sentinel errors (`ErrBitDepth`, `ErrReadFailure`) with proper wrapping via `fmt.Errorf("%w: %w", ...)`. Close errors propagated. Bit depth validated at construction time.

**Panics are unreachable.** `BytesPerSample()`, `interleave()`, and `deinterleave()` panic on unsupported bit depth, but the constructor rejects invalid depths before the Decoder is created. These are defensive assertions, not reachable from the public API.

### Issues

**Encode-path allocations.** `deinterleave()` allocates `make([][]int32, nChannels)` plus per-channel `make([]int32, blockSize)` on every call (once per block). No buffer reuse. Not critical — encoding performance is secondary — but inconsistent with the zero-allocation discipline on the decode path.

### Observations

**API asymmetry.** Decode has streaming (`NewDecoder` + `Read`) and one-shot (`Decode`). Encode is one-shot only. A streaming encoder would require the caller to provide PCM in frame-sized chunks, which is less natural for the encoding use case. The asymmetry is justified.

**CLI WAV writer.** 44-byte RIFF header, no extensible format. Handles container bit depth mapping (20-bit stored as 24-bit, 12-bit as 16-bit, 4-bit as 8-bit). Correct and minimal.

## 3. Test infrastructure

### Conformance (`TestConformance`)

1057 subtests: 6 bit depths x 11 sample rates x 8 channel counts x 3 encoders (where supported).

Each test:
1. Generates white noise PCM.
2. Encodes with selected encoder (saprobe, flac binary, or ffmpeg).
3. Decodes with every supported decoder.
4. Compares decoded PCM against original source.
5. Cross-compares all decoder outputs.

ffmpeg multichannel remapping is handled via `ffmpegMultichannelFails()` skip table, well-documented in `docs/QA.md`.

### IETF conformance (`TestIETFConformance`)

Runs the [ietf-wg-cellar/flac-test-files](https://github.com/ietf-wg-cellar/flac-test-files) testbench:
- **subset** (64 files): byte-exact match against reference flac binary + ffmpeg.
- **uncommon** (11 files): best-effort, no crash expected. Panic recovery with `defer/recover`.
- **faulty** (11 files): must not crash. Error return expected.

Auto-downloads the test repo on first run. All passing (see `docs/QA.md` for per-file status).

### Benchmarks

Three benchmark tests with 10 iterations each, full statistics (median/mean/stddev/min/max):
- `TestBenchmarkEncode`: synthetic white noise, 4 formats, 3 encoders.
- `TestBenchmarkDecode`: synthetic white noise, 4 formats, 4 decoders (including CoreAudio via CGO).
- `TestBenchmarkDecodeFile`: real FLAC files via `BENCH_FLAC_FILE` env var.

`hack/bench.sh` wraps profiling: CPU + memory profiles, top-20 output, PNG call graphs.

### Strengths

- Comprehensive matrix coverage (1057 + 86 IETF subtests).
- Three independent reference implementations for comparison.
- Benchmark infrastructure is reusable and well-factored via agar library.
- Panic recovery in uncommon/faulty tests prevents test suite crashes.
- All tests run in parallel where possible.

### Issues

- **`tests/default.pgo` present.** Leftover from PGO investigation. Should be deleted.
- **No API boundary negative tests.** No tests for invalid `PCMFormat` passed to `Encode`, nil `ReadSeeker` passed to `NewDecoder`, zero-length PCM, or `PCMFormat` with mismatched bit depth. The IETF faulty suite tests the underlying library's parser, but not saprobe-flac's own API surface.
- **Hardcoded ffmpeg skip table.** `ffmpegMultichannelFails()` encodes known ffmpeg behavior per bit-depth/channel combination. If ffmpeg fixes its channel remapping in a future version, tests will silently skip valid comparisons instead of catching the improvement.

## 4. CI/CD

### Workflows

**ci.yml** (push/PR to main):
- **Lint:** Ubuntu 24.04, Go 1.25.6, full `make lint` (golangci-lint, govulncheck, commit validation, license checks, headers, YAML, shell).
- **Test:** 4-platform matrix (ubuntu-24.04, ubuntu-24.04-arm, macos-15, windows-2025). Installs ffmpeg per platform. Runs `make test` (unit + race + bench + coverage).
- **Build:** Same 4-platform matrix. `make build` + `make verify` (runs binary with `--version`).
- Aggregation gates: `test-success` and `build-success` jobs require all matrix entries to pass.

**go-latest.yml** (weekly Monday 9 AM UTC + manual):
- Same lint/test/build matrix but with `go-version: 'stable'` instead of pinned version.
- Catches Go tip regressions before they block development.

### Strengths

- SHA-pinned actions (no tag-based supply chain risk).
- 4-platform coverage including ARM.
- Weekly Go canary catches compatibility issues early.
- Race detector runs in CI (`make test` includes `test-unit-race`).
- Dependabot configured for dependency updates.

### Issues

- **`ffmpeg-full` Homebrew formula.** macOS CI installs `ffmpeg-full` which is a third-party tap formula, not the standard `ffmpeg` formula. Should verify this formula exists on GitHub-hosted macOS runners.
- **IETF conformance not in CI.** `TestIETFConformance` requires the reference `flac` binary and a git clone of the test repo. Neither is set up in CI. The 1057-subtest synthetic conformance suite runs, but IETF spec compliance is only verified locally.

## 5. Linting

### Configuration

golangci-lint v2 with `default: all` — starts from the full set and explicitly enables/disables.

**Notable enabled linters:**
- `gosec` (security), `err113` (static errors), `wrapcheck` (error wrapping), `depguard` (import restrictions)
- `paralleltest`, `tparallel`, `thelper` (test discipline)
- `revive` with `enable-all-rules` and tuned thresholds (function-length: 100, cyclomatic: 30, cognitive: 30)
- `forbidigo` blocks `fmt.Print*` in non-test code
- `wsl_v5` for whitespace formatting
- `funcorder` for public-before-private method ordering

**Formatters:** `gci` (import grouping), `gofumpt` (strict formatting), `golines` (120 char line limit).

**Test relaxations:** `varnamelen`, `dupl`, `wrapcheck`, `gosec`, `err113`, `modernize`, `dogsled`, `noctx`, `perfsprint`, `depguard` disabled in `_test.go`. Appropriate — test code has different ergonomic needs.

### Issues

- **`gomoddirectives` disabled.** Comment says "Until we upstream patches on flac." This suppresses warnings about `replace` directives in `go.mod`. Acceptable given the active fork, but should be re-enabled when the fork situation resolves (either upstream merge or permanent fork declaration).

## 6. Build infrastructure

### Makefile

Shared `hack/common.mk` provides the full task suite:
- `make lint`: golangci-lint (cross-OS: darwin/linux/freebsd/windows) + govulncheck + commit validation + license checks + headers + YAML + shell.
- `make test`: unit + race + bench + coverage.
- `make build`: PIE release binaries with version injection via ldflags.
- `make build-debug`: debug binaries with `-N -l` (no optimization, no inlining).
- `make init-dev`: full developer environment setup (system deps + Go tools).
- `make fix`: auto-fix linting issues + `go mod tidy` + header fixes.

**Build hardening (CGO):**
- `-Wall -Werror=format-security`
- `-fstack-protector-strong -fPIE -D_FORTIFY_SOURCE=2`
- Linux: `-fstack-clash-protection`, `-Wl,-z,defs -Wl,-z,relro -Wl,-z,now -Wl,-z,noexecstack`

**Pure Go by default:** `CGO_ENABLED=0` with `-tags=netgo,osusergo`. CGO enabled only for test targets (CoreAudio benchmarks).

Dev tool installation pins exact commit SHAs (not tags) for golangci-lint, govulncheck, git-validation, ltag, go-licenses.

## 7. Dependencies

| Dependency | Type | Purpose |
|------------|------|---------|
| `github.com/mewkiz/flac` (mycophonic fork) | Runtime | FLAC parsing, frame decoding/encoding |
| `github.com/mycophonic/agar` | Test-only | ffmpeg/CoreAudio wrappers, test helpers, benchmark infrastructure |
| `github.com/icza/bitio` | Transitive (via flac) | Bit-level I/O |
| `github.com/mewkiz/pkg` | Transitive (via flac) | Utility types |
| `github.com/containerd/nerdctl/mod/tigron` | Transitive (via agar) | Test assertion helpers |
| `github.com/creack/pty` | Transitive (via agar) | PTY support for test harness |
| `golang.org/x/sync`, `sys`, `term`, `text` | Transitive | Standard extensions |

`depguard` restricts imports to: stdlib, `saprobe-flac`, `mewkiz/flac`, `agar`.

## 8. Documentation

| Document | Quality | Notes |
|----------|---------|-------|
| `README.md` | Good | Concise, accurate API reference, dependency documentation, support matrix |
| `docs/QA.md` | Excellent | Full test matrix, IETF per-file status, benchmark tables with real files, CPU/memory profiles |
| `docs/OPTIM.md` | Excellent | Optimization investigation diary with 5 rejected approaches, rationale, and remaining options |
| `docs/research/ENCODERS.md` | Good | FLAC encoder landscape analysis |
| `docs/research/DECODERS.md` | Good | FLAC decoder landscape analysis |

## 9. Security

- No user input parsing beyond file path (CLI) and FLAC stream (library). File paths come from CLI args only.
- No network I/O.
- CGO disabled for production builds. Enabled only in test benchmarks with full hardening flags.
- Bit depth validated at construction; unreachable panics as defense-in-depth.
- SHA-pinned CI actions and dev tool installations.
- License compliance enforced via `go-licenses` in CI.
- `gosec` linter enabled.
- No secrets or credentials in codebase.

## 10. Actionable items

| # | Severity | Item | Details |
|---|----------|------|---------|
| 1 | Cleanup | Delete `tests/default.pgo` | Leftover from PGO investigation, no benefit |
| 2 | Low | Add API boundary tests | Negative tests for invalid PCMFormat, nil ReadSeeker, zero-length PCM, mismatched bit depth in Encode |
| 3 | Low | Verify `ffmpeg-full` Homebrew formula in CI | macOS CI uses `brew install ffmpeg-full` — confirm availability on GitHub-hosted runners |
| 4 | Info | No streaming encoder | Asymmetric with decoder API. Justified by use case but worth noting |
| 5 | Info | `gomoddirectives` disabled | Acceptable while fork exists. Re-enable when fork status resolves |
| 6 | Info | IETF conformance not in CI | Requires flac binary + git clone. Only local verification currently |
| 7 | Info | Encode-path per-block allocation | `deinterleave` allocates per call. Could reuse buffers for consistency with decode path |
