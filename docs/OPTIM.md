# FLAC Decode Optimization

## Baseline

Benchmark file: Dolphy — You Don't Know What Love Is (44.1kHz/16bit stereo, 11:22, 54.3 MB FLAC).
Median over 10 iterations.

| Tool | Median | vs saprobe |
|------|--------|------------|
| saprobe | 1.382s | 1.0x |
| flac | 740ms | 1.87x |
| coreaudio | 771ms | 1.79x |
| ffmpeg | 215ms | 6.43x |

## CPU profile

Focused on the Go decoder path (`parseSubframeInto` = 11.03s cumulative across 10 iterations).

| Function | Flat | % decoder | Notes |
|----------|------|-----------|-------|
| decodeLPC | 3.61s | 32.7% | MADD-latency-bound, not optimizable |
| ReadRice (self) | 3.17s | 28.7% | Bit manipulation, buffer management for k low bits |
| ReadUnary (self) | 1.71s | 15.5% | Word-at-a-time zero scanning |
| decodeRicePart (self) | 0.46s | 4.2% | Per-sample loop, error checks, store to Samples[] |
| crc16.Update | 0.46s | 4.2% | Per-byte CRC on small 1-8 byte chunks |
| consumeBytes | 0.33s | 3.0% | Slice creation, flag checks, CRC dispatch |
| interleave | 0.27s | 2.4% | PCM interleave in saprobe wrapper |
| LeadingZeros8 | 0.27s | 2.4% | Bit scan of buffered bits in ReadUnary |
| memmove | 0.23s | 2.1% | Buffer shifts |
| needBytes | 0.23s | 2.1% | Buffer availability check |
| available | 0.21s | 1.9% | `end - pos` subtraction |

### Key insight

The per-sample residual decoding pipeline (ReadRice + ReadUnary + consumeBytes + CRC + needBytes + available + LeadingZeros8) totals **6.84s flat = 62% of decoder time**. This is all overhead from the bit reader's byte-buffer + 0-7 bit remainder architecture: every single Rice residual (thousands per subframe) requires ~6 function calls and 2 CRC updates to extract maybe 6-12 bits.

decodeLPC at 3.61s (33%) is MADD-latency-bound and cannot be improved in pure Go (see rejected investigations below).

## FIR order distribution (real files)

FLAC encoders overwhelmingly use FIR prediction (~99% of subframes). Fixed prediction is rare (<1%). The FIR order distribution varies by content:

| File | Format | Top orders |
|------|--------|------------|
| Dolphy — You Don't Know What Love Is | 44.1/16 CD | 12 (75.5%), 10 (19.5%), 7 (3.8%) |
| Booker's Waltz | 44.1/16 CD | 12 (25.1%), 8 (19.0%), 6 (11.3%), 11 (9.8%), 7 (9.4%) |
| Concert of new music — Side 4 | 44.1/16 vinyl | 8 (87.6%), 7 (10.6%), 6 (1.8%) |
| Morcheeba — Over and Over | 96/24 vinyl | 8 (47.9%), 7 (21.8%), 6 (11.9%), 5 (9.6%) |

Orders 8 and 12 together cover 70-95% of subframes across all tested material.

## Status

No further pure-Go optimizations identified. The decoder is within ~1.9x of the C reference implementation (`flac`), and the remaining time is split between MADD-latency-bound LPC (33%) and irreducible per-sample bit reader work (62%). PGO confirmed that function call overhead is not a bottleneck — the compiler inlines the entire hot path with zero measurable improvement.

## Remaining options

### Frame-level parallelism

FLAC frames are independent. Decode N frames concurrently on separate goroutines. Doesn't reduce per-frame latency but improves wall-clock throughput on multi-core. This is an architectural change to the caller (saprobe), not the decoder library itself.

Expected gain: scales with cores for throughput workloads. Risk: low (frames are independent). Effort: moderate (frame boundary detection, ordered reassembly).

### Assembly

Hand-written arm64/amd64 `.s` files for the rice decode inner loop. Bypasses Go calling convention, bounds checks, and enables hardware-specific tricks. Could potentially match C reference performance.

Expected gain: up to 1.87x (match C). Risk: high (platform-specific, maintenance burden). Effort: very high.

## Investigated and rejected

### LPC unrolling for FIR orders 8 and 12

Target: `flac/frame/subframe.go:decodeLPC`

Hypothesis: Unrolling the inner coefficient loop for common FIR orders (8, 12) into explicit MADD chains with register-cached coefficients would reduce instruction count by ~2.7x for the LPC computation, yielding ~8-15% overall improvement.

Result: **No improvement. Specialized versions are ~15% SLOWER than the generic loop.**

Micro-benchmark (4096 samples, Apple M1 Max):

| Function | ns/op |
|----------|-------|
| decodeLPC8 (unrolled) | 28,800 |
| decodeLPCGeneric order 8 | 25,000 |
| decodeLPC12 (unrolled) | 34,100 |
| decodeLPCGeneric order 12 | 30,200 |

Root cause: The LPC computation is **MADD-latency-bound**, not instruction-count-bound. Each sample requires an 8-element (or 12-element) multiply-accumulate chain where each MADD depends on the previous result. On Apple M1, MADD latency is 3 cycles, giving a fixed minimum of 24 cycles (order 8) or 36 cycles (order 12) per sample regardless of how the code is structured.

The generic version's "extra" instructions (inner loop control, bounds checks, coefficient loads from memory) execute in parallel on the OOO engine while waiting for the MADD chain. They add zero wall-clock time. The unrolled version's larger code footprint actually hurts icache utilization and register allocation, making it slightly slower.

This is fundamentally different from ALAC's `unpcBlock` specializations, which use int32 operations (1-cycle latency on M1) where loop overhead is a meaningful fraction of total work.

### CRC-16 slicing-by-4 (standalone)

Target: `flac/internal/hashutil/crc16/crc16.go:Update`

Hypothesis: Processing 4 bytes per iteration with 4×256 lookup tables would yield 3-4x throughput on CRC computation, ~2-3% overall.

Result: **Not viable with the current bit reader.** CRC-16 is called from `consumeBytes()` which processes 1-8 byte chunks. The 4-byte minimum for slicing-by-4 is rarely met.

### 64-bit shift register bit reader

Target: `flac/internal/bits/reader.go`, `unary.go`, `rice.go`

Hypothesis: Replacing the byte-buffer + 0-7 bit remainder architecture with a 64-bit MSB-aligned shift register would eliminate per-sample function call overhead (needBytes, consumeBytes, CRC dispatch) that accounts for 62% of decoder time (6.84s flat across 10 iterations). ReadUnary would scan the register directly with LeadingZeros64, and ReadRice would extract k low bits with a single shift/mask after ensureBits.

Result: **17% slower. Shift register median 1.621s vs baseline 1.382s (2.17x vs flac, baseline 1.87x).**

Profile (10 iterations, Apple M1 Max):

| Function | Baseline | Shift register | Delta |
|----------|----------|----------------|-------|
| ReadRice | 3.17s | 2.00s | -1.17s |
| ReadUnary | 1.71s | 1.16s | -0.55s |
| consume() | — | 2.53s | +2.53s |
| ensureBits() | — | 0.88s | +0.88s |
| refill() | — | 0.82s | +0.82s |
| flushCRC() | — | 0.78s | +0.78s |
| decodeRicePart | 0.46s | 1.08s | +0.62s |

Root cause: The shift register creates **serial data dependencies** that prevent out-of-order execution from overlapping operations. Each `consume(n)` modifies the register (`bits <<= n; nbits -= n`) and the CRC ring buffer state (`regOff += n`), creating a dependency chain. The old byte-buffer architecture used independent array loads (`br.buf[br.pos]`) that the OOO engine could overlap with unrelated work.

Total per-sample work is identical (~21ns) — the operations move between functions but don't disappear. The shift register trades cache-friendly sequential array access for register-pressure-heavy scalar operations that serialize on the OOO engine.

A deferred-CRC variant (batching CRC at refill boundaries instead of per-byte in consume) was also tested with no meaningful improvement.

### PGO (Profile-Guided Optimization)

Hypothesis: Feeding a CPU profile to the Go compiler (`-pgo`) would improve inlining decisions for the deep call chain (decodeRicePart → ReadRice → ReadUnary → consumeBytes → needBytes → available), yielding 5-10% improvement with zero code changes.

Result: **No measurable improvement. Median 1.38s with PGO vs 1.38s baseline.**

The compiler aggressively inlined the entire hot path with PGO's increased budget (2000 vs default 80):
- `ReadUnary` (cost 551) inlined into `ReadRice`
- `needBytes` (cost 112) inlined into `ReadRice`
- `ReadRice` (cost 966) inlined into `decodeRicePart`
- `decodeRicePart` (cost 1603) inlined into `decodeResiduals`
- `decodeLPC` (cost 285) inlined into `decodeFIR`
- `parseSubframeInto` (cost 402) inlined into `parseSubframes`

The entire decode pipeline became one giant inlined function — and performance was identical. This confirms that **function call overhead is not a bottleneck**. The per-sample work (~21ns) is genuine bit manipulation and memory access that cannot be reduced by eliminating call frames.

This also invalidates the "bulk rice partition decode" hypothesis, which targeted the same call-chain overhead that PGO eliminated with no effect.

### Not pursued

- **SIMD for LPC**: Loop-carried dependency (Samples[i] depends on Samples[i-1]) prevents cross-sample parallelism. Primordium's existing SIMD is float32-only. Not applicable.
- **Fixed prediction specialization**: Fixed prediction is <1% of subframes in real files. Not worth the code.
- **Bulk rice partition decode**: Targeted per-sample call-chain overhead. PGO proved this overhead is zero — the compiler already inlines the entire hot path with PGO, with no improvement. The per-sample work is irreducible bit manipulation, not function call overhead.

## Progress

| Phase | Status | Median | vs flac | Notes |
|-------|--------|--------|---------|-------|
| Baseline | Done | 1.382s | 1.87x | |
| LPC unrolling | Rejected | — | — | MADD-latency-bound, unrolled is 15% slower |
| CRC-16 slicing | Rejected | — | — | Chunks too small with byte-buffer reader |
| Shift register bit reader | Rejected | 1.621s | 2.17x | Serial register dependencies prevent OOO overlap; 17% slower |
| PGO | Rejected | 1.38s | 1.87x | Full hot-path inlining, zero improvement; call overhead is not the bottleneck |
