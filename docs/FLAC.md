# FLAC Support Matrix

## Take away

Support is complete, except 4 bits (who needs that really).

Decoding performance has been squeezed as much as possible in pure Go (~1.93x reference C flac decoder).
The remaining gap is dominated by the inherently serial Rice/LPC decode path and bit-level I/O overhead.

Encoder has not been optimized (still baseline upstream).

## Encoders & Decoders

| Tool        | Role            | Bit Depths Supported                           |
|-------------|-----------------|------------------------------------------------|
| saprobe     | Encode + Decode | Encode: 8, 12, 16, 20, 24, 32 / Decode: 4-32 |
| flac binary | Encode + Decode | Encode: 8, 16, 24, 32 / Decode: 8, 16, 24, 32 (raw output limited to these depths) |
| ffmpeg      | Encode + Decode | Encode: 16, 24 / Decode: 8, 16, 24, 32        |

4-bit encoding is not supported by any encoder. The FLAC spec allows 4-bit in StreamInfo but has no frame header bit
pattern for it (the "011" slot is reserved). 4-bit is excluded from the test matrix.

The reference `flac` binary can decode all bit depths internally, but `--force-raw-format` output only supports
8, 16, 24, and 32-bit. It rejects 12-bit and 20-bit raw output, making byte-exact comparison impossible for those depths via the CLI.

## Bit Depth

| Bit Depth | FLAC Spec      | saprobe encode | saprobe decode     | flac binary encode | flac binary decode | ffmpeg encode | ffmpeg decode | Synthetic Tests |
|-----------|----------------|----------------|--------------------|--------------------|--------------------|---------------|---------------|-----------------|
| 8         | Yes            | Yes            | Yes                | Yes                | Yes                | No            | Yes           | 176 pass        |
| 12        | Yes            | Yes            | Yes                | No                 | No (raw output)    | No            | No            | 88 pass         |
| 16        | Yes            | Yes            | Yes                | Yes                | Yes                | Yes (s16)     | Yes           | 264 pass        |
| 20        | Yes            | Yes            | Yes                | No                 | No (raw output)    | No            | No            | 88 pass         |
| 24        | Yes            | Yes            | Yes                | Yes                | Yes                | Yes (s32)     | Yes           | 264 pass        |
| 32        | Yes (RFC 9639) | Yes            | Yes (patched fork) | Yes                | Yes                | No            | Yes           | 176 pass        |

## Channels

saprobe and the flac binary produce identical output matching source PCM for all channel counts. ffmpeg applies channel
layout remapping for multichannel FLAC, producing different byte ordering. These known ffmpeg multichannel disagreements
are skipped in the test suite.

| Channels | saprobe | flac binary | ffmpeg decode (8-bit) | ffmpeg decode (16-bit) | ffmpeg decode (24-bit) | ffmpeg decode (32-bit) |
|----------|---------|-------------|-----------------------|------------------------|------------------------|------------------------|
| 1        | Pass    | Pass        | Pass                  | Pass                   | Pass                   | Pass                   |
| 2        | Pass    | Pass        | Pass                  | Pass                   | Pass                   | Pass                   |
| 3        | Pass    | Pass        | Skipped               | Skipped                | Skipped                | Skipped                |
| 4        | Pass    | Pass        | Skipped               | Skipped                | Skipped                | Skipped                |
| 5        | Pass    | Pass        | Pass                  | Pass                   | Skipped                | Skipped                |
| 6        | Pass    | Pass        | Pass                  | Pass                   | Skipped                | Skipped                |
| 7        | Pass    | Pass        | Pass                  | Skipped                | Skipped                | Pass                   |
| 8        | Pass    | Pass        | Pass                  | Skipped                | Skipped                | Pass                   |

## Sample Rates

All 11 tested sample rates (8000, 11025, 16000, 22050, 32000, 44100, 48000, 88200, 96000, 176400, 192000 Hz) pass for
all bit depths across saprobe and flac binary decoders.

## Synthetic Test Structure

Tests are organized as `TestFLACDecode/{bitDepth}/{encoder}/{sampleRate}_{channels}`.

For each encoded file, **all supported decoders** run and compare:
- Each decoder's output vs original source PCM (bit-for-bit)
- Each decoder's output vs every other decoder's output

Total: 1057 sub-tests, all passing.

ffmpeg multichannel comparisons that fail due to channel layout remapping are skipped (not counted as failures).
saprobe and flac binary match source bit-for-bit in every case.

## IETF Conformance Tests

The `TestFLACConformance` suite runs against the [ietf-wg-cellar/flac-test-files](https://github.com/ietf-wg-cellar/flac-test-files) repository (auto-cloned on first run).
The test files are organized into three categories:

### Subset (mandatory compliance)

Files that every conforming decoder must handle correctly. saprobe is compared byte-for-byte against the reference
`flac` binary and ffmpeg (where available).

| File | Status | Notes |
|------|--------|-------|
| 01-21, 23-36, 38-60, 61, 63-64 | Pass | Byte-exact match with reference |
| 22 (12-bit per sample) | Pass (saprobe) | Reference `flac` binary cannot produce 12-bit raw output; comparison skipped |
| 37 (20-bit per sample) | Pass (saprobe) | Reference `flac` binary cannot produce 20-bit raw output; comparison skipped |
| 62 (predictor overflow, 20-bit) | Pass (saprobe) | Reference `flac` binary cannot produce 20-bit raw output; comparison skipped |

### Uncommon (best-effort)

Files with unusual properties. Both decoders may legitimately fail; the test verifies saprobe doesn't crash and matches
the reference when both succeed.

| File | Status | Notes |
|------|--------|-------|
| 01 (changing samplerate) | Pass | Both decoders fail (acceptable) |
| 02 (increasing channels) | Pass | Both decoders reject (channel count validation) |
| 03 (decreasing channels) | Pass | Both decoders reject (channel count validation) |
| 04 (changing bitdepth) | Pass | Both decoders reject (bit depth validation) |
| 05 (32bps audio) | Pass | Byte-exact match with reference |
| 06 (768kHz samplerate) | Pass | Both succeed, match |
| 07 (15-bit per sample) | Pass | Both succeed, match |
| 08 (blocksize 65535) | Pass | Both succeed, match |
| 09 (Rice partition order 15) | Pass | Both succeed, match |
| 10 (file starting at frame header) | Skipped | No fLaC signature; headerless FLAC not supported (see Open Issues) |
| 11 (unparsable leading data) | Pass | Both decoders fail (acceptable) |

### Faulty (must not crash)

Files with intentional errors. The decoder must return an error or at least not crash.

| File | Status | Notes |
|------|--------|-------|
| 01 (wrong max blocksize) | Pass (no crash) | Metadata-only error; audio frames are valid, decoder succeeds |
| 02 (wrong max framesize) | Pass (no crash) | Metadata-only error; audio frames are valid, decoder succeeds |
| 03 (wrong bit depth) | Pass | Correctly rejected (bit depth mismatch) |
| 04 (wrong channel count) | Pass | Correctly rejected (channel count mismatch) |
| 05 (wrong total samples) | Pass | Correctly rejected (sample count overflow) |
| 06 (missing streaminfo) | Pass | Correctly rejected |
| 07 (metadata before streaminfo) | Pass | Correctly rejected |
| 08 (blocksize 65536) | Pass | Correctly rejected |
| 09 (blocksize 1) | Pass | Correctly rejected |
| 10 (invalid vorbis comment) | Pass (no crash) | Metadata-only error; audio frames are valid, decoder succeeds |
| 11 (incorrect metadata block length) | Pass | Correctly rejected (invalid block type from garbage header) |

## Summary

- **Synthetic test suite:** All 1057 sub-tests pass (0 failures)
- **IETF conformance:** All subset files decode correctly; all faulty files handled without crashes; uncommon files are best-effort
- **Encode:** 8, 12, 16, 20, 24, 32-bit
- **Decode:** 8, 12, 16, 20, 24, 32-bit (4-bit decode works but is excluded from tests since no encoder can produce it)
- **32-bit:** Requires patched mycophonic/flac fork (RFC 9639 support, MidSide decorrelation overflow fix)
- **ffmpeg multichannel:** For certain channel counts (varies by bit depth), ffmpeg produces different PCM byte
ordering due to channel layout remapping. These are skipped in tests. saprobe and flac binary match source bit-for-bit

## Decode Performance

Run `tests/bench.sh [file.flac]` to reproduce. Results below from 10 iterations decoding `_/test.flac`
(The B-52s — "Strobe Light", 4:00, 44.1kHz/16bit stereo, 27 MB FLAC).

### Timing (median)

| Tool    | Median | Mean  | Min   | Max   |
|---------|--------|-------|-------|-------|
| saprobe | 468ms  | 472ms | 460ms | 490ms |
| flac    | 242ms  | 245ms | 238ms | 260ms |
| ffmpeg  | 100ms  | 105ms | 95ms  | 120ms |

Measured with CPU profiling (`bench.sh` includes `-cpuprofile`). saprobe decode is ~1.93x slower than the reference
C flac decoder and ~4.7x slower than ffmpeg (pure Go vs C). The dominant cost is Rice residual decoding (fused
ReadRice), LPC prediction, and bit-level I/O.

### CPU Profile (decode only)

Total samples: 5.53s across 10 decode iterations.

| Function                              | Flat    | Flat%  | Cum      | Cum%   |
|---------------------------------------|---------|--------|----------|--------|
| bits.(*Reader).Read                   | 980ms   | 17.72% | 980ms    | 17.72% |
| frame.(*Subframe).decodeLPC           | 950ms   | 17.18% | 950ms    | 17.18% |
| bits.(*Reader).ReadUnary              | 450ms   | 8.14%  | 450ms    | 8.14%  |
| runtime.memmove                       | 440ms   | 7.96%  | 440ms    | 7.96%  |
| bits.(*Reader).ReadRice               | 320ms   | 5.79%  | 1750ms   | 31.65% |
| crc16.Update                          | 220ms   | 3.98%  | 220ms    | 3.98%  |
| runtime.memclrNoHeapPointers          | 190ms   | 3.44%  | 190ms    | 3.44%  |
| flac.interleave                       | 110ms   | 1.99%  | 110ms    | 1.99%  |
| frame.(*Subframe).decodeRicePart      | 100ms   | 1.81%  | 1850ms   | 33.45% |
| frame.(*Frame).parseSubframeInto      | 60ms    | 1.08%  | 2600ms   | 47.02% |

The I/O chain is a single buffered struct (`bits.Reader` with 4KB buffer and inline CRC). The redundant
`bufio.NewReader` wrapping in `flac.New()`/`Parse()`/`NewSeek()` has been removed — `bits.Reader` already
maintains a 4096-byte read-ahead buffer. `ReadUnary` uses word-at-a-time scanning via `bits.LeadingZeros64`
(8 bytes per iteration) with batch CRC updates over the entire consumed range, instead of per-byte CRC.

Rice residual decoding uses a fused `ReadRice(k)` method that combines ReadUnary + Read(k) + ZigZag decode
into a single call, avoiding per-residual function call overhead. When the k low bits remain in the buffered
byte from ReadUnary, they are extracted inline without re-entering `Read()`.

Sample and subframe buffers are allocated once per `Stream` (sized to `BlockSizeMax * NChannels` from StreamInfo)
and reused across `ParseNext` calls via `frame.ParseInto`. Per-subframe allocations (LPC coefficients, Rice
partitions, verbatim byte buffers) are also reused across frames via grow-only slices on the `Subframe` struct.
`frame.(*Frame).Parse` no longer appears in the allocation profile. All decode functions use indexed writes
instead of append loops, eliminating grow checks and enabling bounds-check elimination.

PCM interleaving uses stereo-optimized fast paths with packed writes (`binary.LittleEndian.PutUint32/PutUint64`)
and BCE hints for all bit depths, reducing interleave from 29.52% to 1.99% flat.

The hot path is now Rice residual decoding (33.45% cum via `decodeRicePart`) through `ReadRice` (31.65% cum),
which fans out to `bits.Reader.Read` (17.72% flat) + `ReadUnary` (8.14% flat). LPC prediction (`decodeLPC`)
is 17.18% flat. `runtime.memmove` (7.96%) is inherent I/O cost from `bytes.Reader.Read` copying into the
bit reader's buffer and is not actionable. Seek table lookup uses binary search (`sort.Search`) instead of
linear scan.

### Memory Profile (alloc_space, decode only)

Total allocated: 2.93 GB across 10 decode iterations.

| Function                         | Flat      | Flat%  | Cum       | Cum%   |
|----------------------------------|-----------|--------|-----------|--------|
| bytes.growSlice                  | 2500 MB   | 85.26% | 2500 MB   | 85.26% |
| flac.Decode                      | 400 MB    | 13.59% | 400 MB    | 13.59% |

`bytes.growSlice` (85%) is from subprocess I/O (flac/ffmpeg binary output capture), not saprobe decode. The only
remaining saprobe decode allocation is `flac.Decode` (400 MB — output buffer assembly). `Frame.Parse` no longer
appears as an allocation source — all per-frame sample and subframe buffers are now reused from the stream.

### Call Graphs

Generated by `tests/bench.sh` via `go tool pprof`.

#### CPU — hot path through the decode I/O chain

![CPU call graph](../../saprobe/docs/decode_cpu.png)

#### Memory (alloc_space) — allocation sources across decode iterations

![Memory allocation call graph](../../saprobe/docs/decode_alloc.png)

## Open Issues

### Feature gap

1. **uncommon/10 — file starting at frame header (no fLaC signature).**

The file contains valid FLAC frames but no `fLaC` magic bytes or StreamInfo metadata block. The flac library requires
the signature in `parseStreamInfo()`. Supporting headerless FLAC streams would require a new entry point that accepts
StreamInfo parameters externally (sample rate, channels, bit depth) and starts parsing frames directly.
Low priority — this is an uncommon edge case not required by the FLAC specification.

