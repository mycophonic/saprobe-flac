# Implementation Notes

## Trailing garbage tolerance (ID3v1 tags)

### Problem

FLAC files in the wild frequently contain non-standard trailing data appended after the audio stream, most commonly ID3v1 tags (128 bytes). Some files also have ID3v2 tags prepended, but those are handled by the underlying library. The trailing data is the problem: when a FLAC decoder finishes decoding all audio frames and attempts to parse the next frame, it encounters bytes that are not a valid FLAC frame header and reports a hard error.

This affects all three major FLAC decoders:

| Decoder | Behavior |
|---------|----------|
| **libFLAC** (`flac -t`) | `FLAC__STREAM_DECODER_ERROR_STATUS_LOST_SYNC` — reported as `ERROR during decoding` |
| **ffmpeg** | `invalid sync code` / `invalid frame header` / `decode_frame() failed` — exit code 1 |
| **saprobe-flac** (before fix) | `read failure: frame.Frame.parseHeader: invalid sync-code` — exit code 1 |

In all three cases, the audio data is fully and correctly decoded before the error occurs. The error is triggered only when the decoder attempts to read past the last valid frame and encounters the trailing ID3v1 bytes. Despite the audio being intact, the error causes the decode to be reported as a failure.

### Solution

The FLAC StreamInfo metadata block contains `NSamples`: the total number of inter-channel samples in the stream. This value is written by the encoder at encode time and is authoritative.

saprobe-flac now tracks the number of samples delivered during streaming decode. When a parse error occurs after all declared samples have been delivered (`samplesRead >= totalSamples`), the error is treated as a non-fatal end-of-stream condition:

- An `slog.Warn` is emitted with the expected/decoded sample counts and the underlying error, so the trailing data is not silently swallowed.
- `io.EOF` is returned instead of an error, so callers (including `io.ReadAll`) receive complete, valid PCM without interruption.
- If `NSamples` is 0 (unknown), the original hard-error behavior is preserved — we only apply the tolerance when we have a known sample count to compare against.

### Performance impact

One `uint64` addition per frame (incrementing the sample counter by `blockSize`). The frame decode itself performs orders of magnitude more work. Negligible.

### Result

saprobe-flac now successfully decodes FLAC files with trailing ID3v1 tags that cause hard failures in both `flac` (libFLAC reference decoder) and `ffmpeg`. The audio output is byte-identical to what would be produced without the trailing garbage.
