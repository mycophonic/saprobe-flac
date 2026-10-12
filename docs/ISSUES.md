# Known Issues

## TSAN Fork-Child Crash in Race-Detector Conformance Tests

**Status:** Open (Go runtime / TSAN bug)
**Affected:** `just test` → the race lane (`just do test go race`) → `TestConformance`
**Severity:** Flaky test — no production impact
**Go version:** 1.26.0 darwin/arm64
**Frequency:** ~40% failure rate with default parallelism (GOMAXPROCS=10)

### Symptom

`TestConformance` subtests fail non-deterministically under the race detector with errors like:

```
synthetic_test.go:160: flac decode: flac decode: signal: segmentation fault
```

Different subtests fail each run. The failures only occur with `-race`; non-race tests pass consistently.

### Root Cause

The crash is **not** in the `flac` or `ffmpeg` binaries. macOS crash reports (`~/Library/Logs/DiagnosticReports/tests.test-*.ips`) reveal:

```json
{
  "exception": {
    "type": "EXC_BAD_ACCESS",
    "subtype": "KERN_INVALID_ADDRESS at 0x0000000000000008"
  },
  "asi": {
    "libsystem_c.dylib": ["crashed on child side of fork pre-exec"]
  }
}
```

The crash stack shows `__tsan::TraceSwitchPartImpl(__tsan::ThreadState*)` — the Thread Sanitizer (TSAN, which implements Go's `-race` detector) dereferences a near-NULL `ThreadState` pointer in the forked child process before `exec()` replaces the process image.

The mechanism:

1. Go's `exec.Command` uses `fork()+exec()` to spawn child processes.
2. Between `fork()` and `exec()`, the child is a copy of the parent, including TSAN's instrumentation state.
3. TSAN's `TraceSwitchPartImpl` accesses thread tracking structures that are invalid in the forked child.
4. The child crashes with SIGSEGV before `exec()` ever runs.

### Why saprobe-flac Only

`TestConformance` generates ~1057 parallel subtests (4 bit depths x 3 encoders x N sample rates x up to 8 channels), each calling `exec.Command` to fork `flac` and/or `ffmpeg`. This is uniquely high among saprobe repos:

| Repo | Parallel subtests forking external tools |
|------|----------------------------------------|
| saprobe-flac | ~1057 |
| saprobe-mp3 | ~few dozen |
| saprobe-alac | ~few |
| saprobe-dsd | 0 (pure Go tests) |

The TSAN crash is probabilistic — more concurrent `fork()+exec()` calls widen the race window.

### Known Upstream Bug

This is a documented TSAN limitation:

- **[google/sanitizers#945](https://github.com/google/sanitizers/issues/945):** TSAN deadlocks/crashes in `TraceSwitch` on the child side of fork. Fixed in LLVM r331163 by disabling trace switching when `after_multithreaded_fork` is set. The segfault variant (NULL dereference at address 0x8) appears to be a separate manifestation not fully addressed by that fix.
- **[google/sanitizers#255](https://github.com/google/sanitizers/issues/255):** Forking threaded processes under sanitizers is fundamentally unsupported. Marked WontFix.

No Go-specific issue was found tracking this on Go 1.26 darwin/arm64.

### Workarounds

Limit test parallelism to reduce the fork concurrency window:

```bash
# Passes consistently:
CGO_ENABLED=1 go test -ldflags="-linkmode=external" -race -parallel 4 -run TestConformance ./tests/

# Fails ~40% of the time (default GOMAXPROCS=10):
CGO_ENABLED=1 go test -ldflags="-linkmode=external" -race -run TestConformance ./tests/
```

### Reproduction

```bash
# from the repository root
for i in $(seq 1 10); do
  printf "Run %2d: " "$i"
  CGO_ENABLED=1 go test -count=1 -ldflags="-linkmode=external" \
    -timeout 120s -race -run TestConformance ./tests/ 2>&1 \
    | grep -oE "^(ok|FAIL)" | head -1
done
```
