# FLAC Decoders: Prevalence and Technical Specifications

## Overview

FLAC decoding is computationally cheap — by design, the format was optimized for fast decoding at the expense of encoding speed. All major decoders support the full FLAC specification (4–32 bit, 1–8 channels, any sample rate). Unlike FLAC encoding where compression varies between implementations, decoding is deterministic: a correct decoder produces bit-perfect output.

The FLAC decoder testbench (github.com/ietf-wg-cellar/flac-test-files) provides conformance test vectors.

---

## Decoder Comparison Table

| Rank | Decoder | Bit Depth | Sample Rate | Channels | Platform | Notes |
|------|---------|-----------|-------------|----------|----------|-------|
| 1 | **libFLAC (reference)** | 4–32 | 1 Hz – 1,048,575 Hz | 1–8 | Cross-platform | Reference implementation. BSD license. Used by most software. |
| 2 | **FFmpeg (libavcodec)** | 4–32 | Any | 1–8 | Cross-platform | Independent implementation. Powers VLC, mpv, Kodi, Plex, Jellyfin. Has `use_buggy_lpc` option for old broken FFmpeg-encoded streams. |
| 3 | **Apple CoreAudio** | 4–32 | Standard rates | 1–8 | macOS 10.15+, iOS 13+ | Native OS codec. Used by Apple Music, iTunes, QuickTime. |
| 4 | **Windows Media Foundation** | 16, 24 | Standard rates | 1–8 | Windows 10+, Xbox | Native OS codec since Windows 10. Used by Windows Media Player, Groove, Edge. |
| 5 | **Android MediaCodec** | 16, 24 | Standard rates | 1–8 | Android 3.1+ | Native since Android 3.1 (Honeycomb). FLAC-in-MP4 since Android 10. |
| 6 | **dr_flac** | 4–32 | Any | 1–8 | Cross-platform | Single-header C library by David Reid. Public domain (or MIT). Popular in game engines and embedded. |
| 7 | **foobar2000** | 4–32 | Any | 1–8 | Windows | Uses libFLAC internally. Gold standard for desktop FLAC playback on Windows. |
| 8 | **CUETools libFlake decoder** | 4–32 | Any | 1–8 | Windows (.NET) | Pure C# implementation. Part of CUETools. |
| 9 | **Rockbox** | 16, 24 | Up to 96 kHz typical | 1–2 | Embedded/portable | Uses libFLAC-derived code with ARM/ColdFire assembly. Heavily optimized for low-power. Stereo only (hardware constraint). |

---

## Detailed Notes

### libFLAC — Reference Implementation (Rank 1)
- **Usage**: The foundation. Used by foobar2000, EAC, XLD, dBpoweramp, mpd, libsndfile, and the vast majority of FLAC-playing software. If software "plays FLAC," it probably uses libFLAC.
- **Maintained by**: Xiph.Org Foundation (GitHub: xiph/flac).
- **Performance**: Very fast. Decoding speed barely varies across compression levels 0–8 (by design). SIMD optimizations: SSE2, SSSE3, AVX2, NEON.
- **Features**: Streaming decode, seeking (with or without seektable), metadata access, MD5 verification.
- **Embedded use**: Can be stripped to decode-only by removing encoder and metadata editing modules. Binary can be as small as ~100 KiB.
- **License**: BSD (Xiph.org variant). Very permissive — usable anywhere.

### FFmpeg (Rank 2)
- **Usage**: Powers the majority of media applications that aren't using libFLAC directly. VLC, mpv, Kodi, Plex, Jellyfin, MPlayer, and hundreds of others.
- **Implementation**: Fully independent, not a wrapper around libFLAC.
- **Gotcha**: `use_buggy_lpc` option exists because old versions of FFmpeg's FLAC *encoder* produced broken streams. The decoder has this flag to correctly play those old broken files back.
- **Bit depth conversion**: When using FFmpeg to convert FLAC to PCM (e.g., WAV), **you must specify the output format explicitly**, or it may silently truncate. Use: `ffmpeg -i input.flac -c:a pcm_s24le output.wav` for 24-bit source, or `pcm_s32le` for 32-bit.

### Platform Codecs (Ranks 3-5)
- **Apple CoreAudio**: Native since macOS 10.15 Catalina / iOS 13 (2019). Before that, FLAC was not natively supported on Apple platforms — a major pain point for years. Now fully integrated.
- **Windows Media Foundation**: Native since Windows 10 (2015). Before that, Windows required third-party codecs (e.g., Xiph's DirectShow filters). Supports FLAC in MKV, MP4, and native containers.
- **Android**: Decoding since 3.1 (2011), encoding since 4.1 (2012). FLAC-in-MP4 since Android 10 (2019). ExoPlayer provides additional FLAC support for older Android versions.

### dr_flac (Rank 6)
- **Usage**: Game engines, embedded applications, any project that wants FLAC decode with zero library dependencies.
- **Key feature**: Single-file library. `#define DR_FLAC_IMPLEMENTATION` + `#include "dr_flac.h"` = complete FLAC decoder. Part of the "miniaudio" ecosystem (dr_wav, dr_mp3, dr_flac).
- **Quality**: Full spec support. Handles all bit depths, all channel counts.
- **Performance**: Somewhat slower than libFLAC (no SIMD), but more than adequate for real-time playback.
- **License**: Public domain (Unlicense) or MIT — user's choice.

### Rockbox (Rank 9)
- **Usage**: Open-source firmware for portable players (iPod, Sansa, iRiver, etc.).
- **Implementation**: libFLAC-derived, with heavy ARM and ColdFire assembly optimizations.
- **Performance**: One of the fastest known FLAC implementations on ARM. Cited as "among the fastest known implementations" by Hydrogenaudio. Designed to decode 16/44.1 in real-time on 80 MHz ARM7 processors.
- **Limitation**: Stereo only (hardware constraint of target devices, not a codec limitation). Typically supports up to 24-bit / 96 kHz depending on DAC hardware.

---

## Web Browser Support

All major browsers now support FLAC decoding natively:

| Browser | Since |
|---------|-------|
| Chrome | 56 (2017) |
| Firefox | 51 (2017) |
| Edge | 16 (2017) |
| Safari | 11 (2017) |
| Opera | 43 (2017) |

FLAC can be played via `<audio>` element or Web Audio API. Supported in bare FLAC and Ogg containers.

---

## Summary: Which Decoder to Use?

| Use Case | Recommended | Why |
|----------|-------------|-----|
| Desktop application | **libFLAC** | Reference, fastest, most widely tested, BSD license |
| Media framework/player | **FFmpeg** or **libFLAC** | Both excellent. FFmpeg if already a dependency. |
| Single-header embedding (games, tools) | **dr_flac** | Zero deps, public domain, full spec |
| macOS/iOS app | **CoreAudio** | Native, free, already there |
| Windows app | **Media Foundation** or **libFLAC** | MF for native integration, libFLAC for portability |
| Embedded / portable player | **libFLAC (stripped)** or **Rockbox codec** | Proven on extremely low-power hardware |
| Web | **Browser native** | All major browsers since 2017 |

---

## Decode Performance Notes

FLAC decoding speed is essentially **invariant to compression level**. A file encoded with `-0` (fastest, least compression) decodes at roughly the same speed as one encoded with `-8` (slowest, best compression). This is because the decompression algorithm is the same — only the predictor order and block size change, and decoding these is cheap.

Typical decode speeds (single-core, modern desktop):
- 16/44.1 stereo: 500–1000x real-time
- 24/96 stereo: 200–500x real-time
- 24/192 stereo: 100–250x real-time

On embedded ARM (Cortex-A53, 1.2 GHz single-core):
- 16/44.1 stereo: ~50–100x real-time
- 24/96 stereo: ~20–50x real-time
- 24/96 6-channel: ~5–15x real-time (severe degradation with channel count)