package tests_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mycophonic/agar/pkg/agar"

	flac "github.com/mycophonic/saprobe-flac"
)

// flacEncoderType identifies a FLAC encoder.
type flacEncoderType int

const (
	encoderSaprobe flacEncoderType = iota
	encoderFlacBinary
	encoderFFmpeg
)

// flacDecoderType identifies a FLAC decoder.
type flacDecoderType int

const (
	decoderSaprobe flacDecoderType = iota
	decoderFlacBinary
	decoderFFmpeg
)

// All encodable FLAC bit depths (4-bit excluded: no frame header bit pattern in FLAC spec).
//
//nolint:gochecknoglobals
var allFlacBitDepths = []int{8, 12, 16, 20, 24, 32}

// flacSampleRates covers the full range of commonly used sample rates.
//
//nolint:gochecknoglobals
var flacSampleRates = []int{
	8000, 11025, 16000, 22050, 32000, 44100, 48000, 88200, 96000, 176400, 192000,
}

// flacChannelCounts covers all FLAC-supported channel counts (1 through 8).
//
//nolint:gochecknoglobals
var flacChannelCounts = []int{1, 2, 3, 4, 5, 6, 7, 8}

// encodersForBitDepth returns which encoders support the given bit depth.
func encodersForBitDepth(bitDepth int) []flacEncoderType {
	switch bitDepth {
	case 12, 20:
		return []flacEncoderType{encoderSaprobe}
	case 8, 32:
		return []flacEncoderType{encoderSaprobe, encoderFlacBinary}
	case 16, 24:
		return []flacEncoderType{encoderSaprobe, encoderFlacBinary, encoderFFmpeg}
	default:
		return nil
	}
}

// decodersForBitDepth returns which decoders support the given bit depth.
func decodersForBitDepth(bitDepth int) []flacDecoderType {
	switch bitDepth {
	case 12, 20:
		return []flacDecoderType{decoderSaprobe}
	case 8, 16, 24, 32:
		return []flacDecoderType{decoderSaprobe, decoderFlacBinary, decoderFFmpeg}
	default:
		return nil
	}
}

func encoderName(enc flacEncoderType) string {
	switch enc {
	case encoderSaprobe:
		return "saprobe"
	case encoderFlacBinary:
		return "flac"
	case encoderFFmpeg:
		return "ffmpeg"
	default:
		return "unknown"
	}
}

func decoderName(dec flacDecoderType) string {
	switch dec {
	case decoderSaprobe:
		return "saprobe"
	case decoderFlacBinary:
		return "flac"
	case decoderFFmpeg:
		return "ffmpeg"
	default:
		return "unknown"
	}
}

// ffmpegMultichannelFails reports whether ffmpeg's FLAC decoder is known to produce
// different PCM byte ordering for the given bit depth and channel count due to
// channel layout remapping.
func ffmpegMultichannelFails(bitDepth, channels int) bool {
	switch bitDepth {
	case 8:
		return channels >= 3 && channels <= 4
	case 16:
		return (channels >= 3 && channels <= 4) || (channels >= 7 && channels <= 8)
	case 24:
		return channels >= 3
	case 32:
		return channels >= 3 && channels <= 6
	default:
		return false
	}
}

// TestFLACDecode tests all bit depth x encoder x sample rate x channel combinations.
func TestFLACDecode(t *testing.T) {
	t.Parallel()

	flacBin, flacBinErr := agar.LookFor("flac")

	for _, bitDepth := range allFlacBitDepths {
		encoders := encodersForBitDepth(bitDepth)
		decoders := decodersForBitDepth(bitDepth)

		for _, enc := range encoders {
			for _, sampleRate := range flacSampleRates {
				for _, channels := range flacChannelCounts {
					name := fmt.Sprintf(
						"%dbit/%s/%dHz_%dch",
						bitDepth, encoderName(enc), sampleRate, channels,
					)

					t.Run(name, func(t *testing.T) {
						t.Parallel()

						if enc == encoderFlacBinary && flacBinErr != nil {
							t.Skip("standalone flac binary not found")
						}

						runFlacTest(t, enc, decoders, flacBin, bitDepth, sampleRate, channels)
					})
				}
			}
		}
	}
}

//nolint:cyclop // Test orchestration requires many steps.
func runFlacTest(
	t *testing.T,
	enc flacEncoderType,
	decoders []flacDecoderType,
	flacBin string,
	bitDepth, sampleRate, channels int,
) {
	t.Helper()

	tmpDir := t.TempDir()
	srcPCM := generateWhiteNoise(sampleRate, bitDepth, channels, 1)
	srcPath := filepath.Join(tmpDir, "source.raw")

	if err := os.WriteFile(srcPath, srcPCM, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	encPath := filepath.Join(tmpDir, "encoded.flac")

	// Encode with the selected encoder.
	switch enc {
	case encoderSaprobe:
		if err := saprobeFlacEncode(srcPCM, encPath, bitDepth, sampleRate, channels); err != nil {
			t.Fatalf("saprobe encode: %v", err)
		}
	case encoderFlacBinary:
		if err := flacBinaryEncode(flacBin, srcPath, encPath, bitDepth, sampleRate, channels); err != nil {
			t.Fatalf("flac encode: %v", err)
		}
	case encoderFFmpeg:
		var sampleFmt string

		switch bitDepth {
		case 16:
			sampleFmt = "s16"
		case 24:
			sampleFmt = "s32"
		default:
			t.Fatalf("ffmpeg FLAC encoder does not support %d-bit", bitDepth)
		}

		agar.FFmpegEncode(t, agar.FFmpegEncodeOptions{
			Src: srcPath, Dst: encPath,
			BitDepth: bitDepth, SampleRate: sampleRate, Channels: channels,
			CodecArgs: []string{"-c:a", "flac", "-sample_fmt", sampleFmt},
		})
	default:
		t.Fatalf("unknown encoder type: %d", enc)
	}

	// Decode with every supported decoder and compare against source.
	decoded := make(map[string][]byte, len(decoders))

	ffmpegSkip := ffmpegMultichannelFails(bitDepth, channels)

	for _, dec := range decoders {
		if dec == decoderFlacBinary && flacBin == "" {
			t.Log("skipping flac decoder: binary not found")

			continue
		}

		if dec == decoderFFmpeg && ffmpegSkip {
			t.Logf("skipping ffmpeg decode: known channel remapping for %dbit/%dch", bitDepth, channels)

			continue
		}

		decName := decoderName(dec)
		pcm, format := runFlacDecode(t, dec, flacBin, encPath, bitDepth, channels)

		// Verify format metadata (saprobe decoder only, others return raw bytes).
		if dec == decoderSaprobe && format != nil {
			verifyFlacFormat(t, format, sampleRate, bitDepth, channels)
		}

		// Compare decoded PCM vs original source.
		label := fmt.Sprintf("decode(%s) vs source", decName)

		if len(srcPCM) != len(pcm) {
			t.Errorf("%s length mismatch: source=%d, decoded=%d", label, len(srcPCM), len(pcm))
		}

		compareLosslessSamples(t, label, srcPCM, pcm, bitDepth, channels)

		decoded[decName] = pcm
	}

	// Cross-compare all decoder outputs against each other.
	decoderNames := make([]string, 0, len(decoded))
	for name := range decoded {
		decoderNames = append(decoderNames, name)
	}

	for idx := range decoderNames {
		for jdx := idx + 1; jdx < len(decoderNames); jdx++ {
			nameA := decoderNames[idx]
			nameB := decoderNames[jdx]
			label := fmt.Sprintf("decode(%s) vs decode(%s)", nameA, nameB)

			if len(decoded[nameA]) != len(decoded[nameB]) {
				t.Errorf("%s length mismatch: %s=%d, %s=%d",
					label, nameA, len(decoded[nameA]), nameB, len(decoded[nameB]))
			}

			compareLosslessSamples(t, label, decoded[nameA], decoded[nameB], bitDepth, channels)
		}
	}
}

func runFlacDecode(
	t *testing.T,
	dec flacDecoderType,
	flacBin, encPath string,
	bitDepth, channels int,
) ([]byte, *flac.PCMFormat) {
	t.Helper()

	switch dec {
	case decoderSaprobe:
		pcm, format, err := decodeFlacFile(encPath)
		if err != nil {
			t.Fatalf("saprobe decode: %v", err)
		}

		return pcm, &format
	case decoderFlacBinary:
		pcm, err := flacBinaryDecodeRaw(flacBin, encPath)
		if err != nil {
			t.Fatalf("flac decode: %v", err)
		}

		return pcm, nil
	case decoderFFmpeg:
		pcm := agar.FFmpegDecode(t, agar.FFmpegDecodeOptions{Src: encPath, BitDepth: bitDepth, Channels: channels})

		return pcm, nil
	default:
		t.Fatalf("unknown decoder type: %d", dec)

		return nil, nil
	}
}

func verifyFlacFormat(t *testing.T, format *flac.PCMFormat, sampleRate, bitDepth, channels int) {
	t.Helper()

	if format.SampleRate != sampleRate {
		t.Errorf("sample rate: got %d, want %d", format.SampleRate, sampleRate)
	}

	if int(format.BitDepth) != bitDepth {
		t.Errorf("bit depth: got %d, want %d", format.BitDepth, bitDepth)
	}

	if int(format.Channels) != channels {
		t.Errorf("channels: got %d, want %d", format.Channels, channels)
	}
}
