# FLAC Encoders: Prevalence and Technical Specifications

## Overview

FLAC (Free Lossless Audio Codec) supports **1–8 channels**, bit depths from **4 to 32 bits**, and sample rates from **1 Hz to 1,048,575 Hz** per the RFC 9639 specification. It is a **lossless** codec — the decoded output is bit-for-bit identical to the input regardless of compression settings. Compression settings only affect file size and encoding speed, never quality.

The FLAC specification defines a **subset** format for maximum compatibility (streamable, hardware-friendly) and a **lax** mode allowing extended parameters.

FLAC is BSD-licensed. No patents, no royalties.

---

## Encoder Comparison Table

| Rank | Encoder | Bit Depth | Sample Rate | Channels | Compression Levels | Platform | Notes |
|------|---------|-----------|-------------|----------|--------------------|----------|-------|
| 1 | **libFLAC (reference)** | 4–32 | 1 Hz – 1,048,575 Hz | 1–8 | 0–8 | Cross-platform | The reference implementation. Everything else is measured against this. BSD license. |
| 2 | **FFmpeg (libavcodec)** | 16, 24, 32 | Any | 1–8 | 0–12 | Cross-platform | Independent implementation in libavcodec. Historically had bugs with high LPC orders — now fixed. Slightly different blocksize defaults than reference for hi-res audio. |
| 3 | **CUETools libFlake** | 16, 24 | CD-focused (44.1, 48 kHz typical) | 1–2 typical | 0–11 | Windows (.NET) | Based on Justin Ruggles' Flake encoder. Faster than libFLAC at equivalent compression. Supports experimental variable blocksize encoding. |
| 4 | **FLACCL (CUETools)** | 16, 24 | CD-focused | 1–2 typical | 0–11 | Windows (.NET + GPU) | GPU-accelerated via OpenCL. ~10x faster than libFLAC at high compression levels. FLACCL -8 is faster than libFLAC -0 while compressing better than libFLAC -8. |
| 5 | **Apple CoreAudio** | 16, 24 | Standard rates | 1–8 | Auto | macOS, iOS | Encoding and decoding since macOS 10.15 / iOS 13. |
| 6 | **Windows Media Foundation** | 16, 24 | Standard rates | 1–2 | Auto | Windows 10+ | Native OS encoder since Windows 10. |
| 7 | **Android MediaRecorder** | 16, 24 | Standard rates | 1–2 | Auto | Android 4.1+ | Encoding support since Android 4.1 (Jelly Bean). |

---

## Historical / Niche Encoders

| Encoder | Era | Notes |
|---------|-----|-------|
| **Flake (Justin Ruggles)** | 2006–2009 | Independent FLAC encoder. First to implement variable blocksize encoding. Faster than libFLAC. Partly merged into FFmpeg's encoder. Basis for CUETools libFlake. Development stalled. |
| **Nayuki's FLAC encoder** | 2016+ | Java. Experimental. Explores variable blocksize with dynamic programming for optimal block sequencing. Produces smallest files in lax mode, but extremely slow (100–1000x slower than libFLAC). Academic/educational. |

---

## Detailed Notes

### libFLAC — Reference Implementation (Rank 1)
- **Usage**: The standard. Used by the `flac` command-line tool, EAC (Exact Audio Copy), dBpoweramp, XLD, foobar2000, and the vast majority of software that encodes FLAC.
- **Maintained by**: Xiph.Org Foundation (GitHub: xiph/flac). Active development.
- **Compression levels 0–8**: Presets for combinations of block size, LPC order, prediction method, and Rice partition order. Level 5 is the default and sweet spot: beyond 5, encoding time increases sharply with diminishing compression gains.
- **Subset vs. Lax**: Subset limits LPC order to ≤12 and block size to ≤4608 (for ≤48 kHz) to ensure hardware compatibility. Lax mode allows LPC order up to 32 and larger block sizes.
- **Features**: MD5 checksum of original audio in STREAMINFO, seektable, Vorbis comment metadata, embedded album art, ReplayGain.
- **SIMD**: SSE2, SSSE3, AVX2, NEON optimizations for both encoder and decoder.
- **Embedded use**: Can be stripped down for decode-only use by removing encoder and metadata modules.

### FFmpeg (Rank 2)
- **Usage**: Available wherever FFmpeg is compiled. Powers transcoding in Plex, Jellyfin, and command-line workflows.
- **History**: Originally based on Flake encoder code (Justin Ruggles). Has diverged significantly.
- **Known issue (historical)**: The FFmpeg FLAC encoder used to produce buggy streams with high LPC values (including the default). A `use_buggy_lpc` compatibility option exists in the decoder to handle these old files. This is fixed in modern FFmpeg.
- **Blocksize quirk**: FFmpeg's FLAC encoder may use different default blocksizes than libFLAC for hi-res audio (24/96, 24/192), which can cause larger files or compatibility issues with some hardware.
- **Compression**: Generally produces slightly larger files than libFLAC at the same conceptual level.

### CUETools libFlake (Rank 3)
- **Usage**: CUETools CD ripping/processing tool. Niche but respected in the audiophile CD ripping community.
- **Implementation**: Pure C# (.NET). Based on Justin Ruggles' Flake, heavily modified by Grigory Chudov.
- **Advantage**: Faster than libFLAC. At equivalent computation time, produces roughly the same compression ratio.
- **Variable blocksize**: Supports `-v 1` and `-v 2` (two heuristics for variable block size encoding). `-v 2` produces smaller files but is ~5x slower than `-v 1`.
- **Levels 9–11**: Non-subset "super-compression" levels. May not play on all hardware decoders. Any compliant software decoder handles them fine.

### FLACCL (Rank 4)
- **Usage**: Enthusiasts who want maximum speed, especially for batch encoding large libraries.
- **How**: Offloads LPC computation to GPU via OpenCL. Requires NVIDIA GeForce 2xx+, AMD Radeon HD 5xxx+, or Intel HD Graphics.
- **Performance**: On a GeForce GTX 285 vs. Core i7 940: FLACCL -8 runs ~10x faster than libFLAC -8, and FLACCL -8 is faster than libFLAC -0 while producing smaller files than libFLAC -8.
- **Limitation**: Windows-only (.NET + OpenCL). CD-focused (16-bit primarily).

---

## Summary: Which Encoder to Use?

| Use Case | Recommended | Why |
|----------|-------------|-----|
| General purpose | **libFLAC -5** (default) | Best compatibility, good compression, fast |
| Maximum compression (subset-safe) | **libFLAC -8** | ~1-2% smaller than -5. Safe for all hardware. |
| Maximum compression (software-only playback) | **CUETools libFlake -11 --lax** | Smallest files, but non-subset. Hardware players may choke. |
| Maximum speed | **FLACCL -8** (GPU) | 10x faster than libFLAC at same compression |
| Cross-platform scripting | **FFmpeg** or **libFLAC** | Both work. libFLAC produces slightly smaller files. |
| CD ripping | **EAC + libFLAC** or **CUETools** | Both use libFLAC or libFlake. CUETools adds AccurateRip verification. |
| Archival | **libFLAC -8** | Proven, subset-compliant, maximum compatibility |

---

## FLAC Format Specification Reference (RFC 9639)

| Parameter | Specification |
|-----------|---------------|
| Bit depths | 4–32 (integer PCM) |
| Sample rates | 1 – 1,048,575 Hz |
| Channels | 1–8 |
| Container formats | Native (.flac), Ogg (.oga), MP4/ISOBMFF |
| Block sizes | 16 – 65,535 samples (subset: ≤4608 for ≤48 kHz, ≤16384 for >48 kHz) |
| LPC order | 0–32 (subset: ≤12 for ≤48 kHz) |
| Compression | ~50-70% of original (content-dependent; CD audio typically ~60%) |
| Integrity | CRC-8 per frame header, CRC-16 per frame, MD5 of entire decoded audio in STREAMINFO |
| Stereo decorrelation | Left/Right, Mid/Side, Left/Side, Right/Side (adaptive per frame) |
| Metadata | Vorbis comments, seektable, album art, application blocks, padding |

### Subset Constraints (for hardware compatibility)

| Parameter | Subset limit |
|-----------|-------------|
| Block size (≤48 kHz) | ≤ 4,608 samples |
| Block size (>48 kHz) | ≤ 16,384 samples |
| LPC order (≤48 kHz) | ≤ 12 |
| Rice partition order | ≤ 15 |
| Sample rate | ≤ 655,350 Hz |
| Bit depth | ≤ 24 (practical; format allows 32) |