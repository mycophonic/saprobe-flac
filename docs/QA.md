# Testing FLAC

## Conformance (`TestConformance`)

Round-trip encode/decode tests across all supported bit depth, sample rate, channel count, and encoder/decoder combinations. Subtests are named `{bitDepth}bit/{encoder}/{sampleRate}Hz_{channels}ch`.

```bash
# Full suite (all tools)
make test

# Conformance only
go test ./tests/ -run TestConformance -count=1 -v

# Single subtest
go test ./tests/ -run TestConformance/16bit/saprobe/44100Hz_2ch -count=1 -v
```

**Test matrix:**

- **Bit depths:** 8, 12, 16, 20, 24, 32
- **Sample rates:** 8000, 11025, 16000, 22050, 32000, 44100, 48000, 88200, 96000, 176400, 192000
- **Channels:** 1-8

Each encoder produces an encoded file which is decoded by all compatible decoders. Decoded output is compared against the original source PCM and cross-compared across decoders.

Total: 1057 sub-tests, all passing.

**Encoder/decoder support by bit depth:**

| Bit Depth | Encoders                   | Decoders                   |
|-----------|----------------------------|----------------------------|
| 8         | saprobe, flac              | saprobe, flac, ffmpeg      |
| 12        | saprobe                    | saprobe                    |
| 16        | saprobe, flac, ffmpeg      | saprobe, flac, ffmpeg      |
| 20        | saprobe                    | saprobe                    |
| 24        | saprobe, flac, ffmpeg      | saprobe, flac, ffmpeg      |
| 32        | saprobe, flac              | saprobe, flac, ffmpeg      |

4-bit encoding is not supported by any encoder. The FLAC spec allows 4-bit in StreamInfo but has no frame header bit pattern for it (the "011" slot is reserved). 4-bit is excluded from the test matrix.

The reference `flac` binary can decode all bit depths internally, but `--force-raw-format` output only supports 8, 16, 24, and 32-bit. It rejects 12-bit and 20-bit raw output, making byte-exact comparison impossible for those depths via the CLI.

**Verification:**

- Mono and stereo: bit-for-bit PCM match against source, plus cross-decoder comparison.
- Multichannel (3-8ch): ffmpeg's FLAC decoder applies channel layout remapping for certain bit depth/channel combinations, causing byte-order differences vs the flac binary and saprobe. These known cases are skipped for ffmpeg cross-comparison; saprobe and the flac binary are always compared.

**ffmpeg multichannel remapping (skipped configurations):**

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

**Reference tools:**

| Tool   | Role            | Bit Depths     | Notes                                         |
|--------|-----------------|----------------|-----------------------------------------------|
| flac   | Encode + Decode | 8, 16, 24, 32  | Reference C implementation (xiph.org)         |
| ffmpeg | Encode + Decode | 16, 24         | Uses libavcodec FLAC encoder/decoder          |

Both tools are optional; tests skip gracefully when unavailable.

## IETF Conformance (`TestIETFConformance`)

Runs the [IETF FLAC decoder testbench](https://github.com/ietf-wg-cellar/flac-test-files) against three decoders: the reference flac binary, ffmpeg, and saprobe.

```bash
go test ./tests/ -run TestIETFConformance -count=1 -v

# With local clone
go test ./tests/ -run TestIETFConformance -conformance-repo=/path/to/flac-test-files -count=1 -v
```

The conformance repo is auto-downloaded if not specified. Three categories:

- **subset:** Standard files that must decode identically across all three decoders.
- **uncommon:** Best-effort decoding; no crash expected.
- **faulty:** Must not crash; decoder errors are expected.

### Subset (mandatory compliance)

Files that every conforming decoder must handle correctly. saprobe is compared byte-for-byte against the reference `flac` binary and ffmpeg (where available).

| File | Status | Notes |
|------|--------|-------|
| 01-21, 23-36, 38-60, 61, 63-64 | Pass | Byte-exact match with reference |
| 22 (12-bit per sample) | Pass (saprobe) | Reference `flac` binary cannot produce 12-bit raw output; comparison skipped |
| 37 (20-bit per sample) | Pass (saprobe) | Reference `flac` binary cannot produce 20-bit raw output; comparison skipped |
| 62 (predictor overflow, 20-bit) | Pass (saprobe) | Reference `flac` binary cannot produce 20-bit raw output; comparison skipped |

### Uncommon (best-effort)

Files with unusual properties. Both decoders may legitimately fail; the test verifies saprobe doesn't crash and matches the reference when both succeed.

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

## Benchmarks

### Synthetic Encode (`TestBenchmarkEncode`)

Benchmarks encoding across saprobe, the flac binary, and ffmpeg using synthetic white noise. Skipped in `-short` mode.

```bash
go test ./tests/ -run TestBenchmarkEncode -count=1 -v
```

- **Formats:** CD (44.1kHz/16bit), HiRes (96kHz/24bit), UltraHiRes (192kHz/24bit), Studio (192kHz/32bit)
- **Duration:** 10 seconds per format
- **Encoders:** saprobe, flac, ffmpeg (16/24-bit only)
- **Iterations:** 10 per configuration
- **Statistics:** median, mean, stddev, min, max

### Synthetic Decode (`TestBenchmarkDecode`)

Benchmarks decoding across all available decoders. Skipped in `-short` mode.

```bash
go test ./tests/ -run TestBenchmarkDecode -count=1 -v
```

- **Formats:** CD (44.1kHz/16bit), HiRes (96kHz/24bit), UltraHiRes (192kHz/24bit), Studio (192kHz/32bit)
- **Duration:** 10 seconds per format
- **Decoders:** saprobe, flac, ffmpeg, coreaudio (macOS only)
- **Iterations:** 10 per configuration
- **Statistics:** median, mean, stddev, min, max

### Real Files (`TestBenchmarkDecodeFile`)

Benchmarks decoding natural FLAC files. 10 iterations, same decoder set and statistics as synthetic benchmarks. Skipped in `-short` mode.

```bash
BENCH_FLAC_FILE='/path/to/file.flac' go test ./tests/ -run TestBenchmarkDecodeFile -count=1 -v
```

Reference files selected for variety in duration, sample rate, and bit depth.
Compression ratio = encoded size / raw PCM size.
Decode times are median over 10 iterations.
Ratio columns show saprobe time relative to each reference tool (>1x = saprobe slower, <1x = saprobe faster).

| File | Format | Duration | Size | saprobe | flac | vs flac | ffmpeg | vs ffmpeg | coreaudio | vs coreaudio | Character |
|------|--------|----------|------|---------|------|---------|--------|-----------|-----------|--------------|-----------|
| Dolphy — You Don't Know What Love Is | 44.1/16 | 11:22 | 54.3 MB | 1.356s | 713ms | 1.9x | 219ms | 6.2x | 757ms | 1.8x | Jazz quartet, moderate density |
| Booker's Waltz | 44.1/16 | 14:36 | 96.7 MB | 1.748s | 928ms | 1.9x | 341ms | 5.1x | 956ms | 1.8x | Long jazz, moderate density |
| Concert of new music — Side 4 | 44.1/16 | 28:55 | 159.0 MB | 3.352s | 1.718s | 2.0x | 452ms | 7.4x | 1.590s | 2.1x | Very long vinyl side, two pianos |
| Morcheeba — Over and Over (vinyl) | 96/24 | 2:21 | 50.7 MB | 664ms | 412ms | 1.6x | 155ms | 4.3x | 416ms | 1.6x | Hi-res vinyl rip, trip-hop |

Saprobe is ~1.6-2.1x slower than the reference flac binary (C) and CoreAudio (CGO, in-process). ffmpeg is 4-7x faster due to SIMD-optimized C. The gap is consistent across CD and hi-res formats.

Full paths:

```
/Volumes/Anisotope/Music/Processor/Dolphy/1-miss_some/Dolphy, Eric/1965-Last Date/[1991-DE-CD-EmArcy-510 124-2-731451012426]/05-06-You Don't Know What Love Is.flac
/Volumes/Anisotope/Music/Processor/Incoming/Fresh/1995 - The Complete Prestige Recordings/Disc7/04 - Booker's Waltz.flac
/Volumes/Anisotope/Music/Baal/need.mb.data.net/x.Mess/1 - needs info/(1978) - A concert of new music for two pianos exploring the history of jazz with love [VINYL]/01(1)-04(4) - Side 4.flac
/Volumes/Anisotope/Music/Processor/Other Music/Morcheeba/1998-Big Calm/[2015-XE-Vinyl-Indochina--0825646134878]/07-11-Over and Over.flac
```

#### CPU Profile

```bash
hack/bench.sh TestBenchmarkDecodeFile '/path/to/file.flac'
```

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

The hot path is Rice residual decoding (33.45% cum via `decodeRicePart`) through `ReadRice` (31.65% cum), which fans out to `bits.Reader.Read` (17.72% flat) + `ReadUnary` (8.14% flat). LPC prediction (`decodeLPC`) is 17.18% flat. `runtime.memmove` (7.96%) is inherent I/O cost from `bytes.Reader.Read` copying into the bit reader's buffer and is not actionable.

#### Memory

Total allocated: 2.93 GB across 10 decode iterations.

| Function                         | Flat      | Flat%  | Cum       | Cum%   |
|----------------------------------|-----------|--------|-----------|--------|
| bytes.growSlice                  | 2500 MB   | 85.26% | 2500 MB   | 85.26% |
| flac.Decode                      | 400 MB    | 13.59% | 400 MB    | 13.59% |

`bytes.growSlice` (85%) is from subprocess I/O (flac/ffmpeg binary output capture), not saprobe decode. The only remaining saprobe decode allocation is `flac.Decode` (400 MB, output buffer assembly). `Frame.Parse` no longer appears as an allocation source.

## Mass testing

Comparative decoding is being done on a set of 6049 FLAC files from the Anisotope collection. Distribution is predominantly CD quality (44.1kHz/16bit stereo) with some hi-res (96kHz/24bit, 192kHz/24bit) vinyl rips.

## Open issues

### Feature gap

1. **uncommon/10 — file starting at frame header (no fLaC signature).**

The file contains valid FLAC frames but no `fLaC` magic bytes or StreamInfo metadata block. The flac library requires the signature in `parseStreamInfo()`. Supporting headerless FLAC streams would require a new entry point that accepts StreamInfo parameters externally (sample rate, channels, bit depth) and starts parsing frames directly. Low priority — this is an uncommon edge case not required by the FLAC specification.
