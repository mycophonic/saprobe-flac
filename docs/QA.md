# Testing FLAC

The codec is tested where it lives: [mycophonic/flac](https://github.com/mycophonic/flac)
holds the IETF conformance sets against the reference decoder and ffmpeg, the
encoder and decoder matrices with a sample-rate row, the seek matrix, the
hostile tables, a random damage sweep, the fuzz targets and the differential
harness (the ffmpeg leg, the sample rates and the damage sweep arrived with
mycophonic/flac#66, the pair of this change). This repository tests the layer it
adds: the Read contract whatever the buffer, the stream's end to the byte,
Close reporting the source's failure, the narrow depths' PCM packing, Encode
refusing what it cannot write, the example decoder's exit codes, a fuzz
target on the decoder, and the benchmarks below.

```bash
make test
```

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
| io.ReadAll (Decoder.Read)        | 400 MB    | 13.59% | 400 MB    | 13.59% |

`bytes.growSlice` (85%) is from subprocess I/O (flac/ffmpeg binary output capture), not saprobe decode. The only remaining saprobe decode allocation is `io.ReadAll` via `Decoder.Read` (400 MB, output buffer assembly). `Frame.Parse` no longer appears as an allocation source.

## Mass testing

Comparative decoding is being done on a set of 6049 FLAC files from the Anisotope collection. Distribution is predominantly CD quality (44.1kHz/16bit stereo) with some hi-res (96kHz/24bit, 192kHz/24bit) vinyl rips.
