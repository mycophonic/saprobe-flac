package saprobe_flac

import "fmt"

// BitDepth represents the bit depth of PCM audio samples.
type BitDepth uint

// Standard PCM bit depths.
const (
	Depth4  BitDepth = 4
	Depth8  BitDepth = 8
	Depth12 BitDepth = 12
	Depth16 BitDepth = 16
	Depth20 BitDepth = 20
	Depth24 BitDepth = 24
	Depth32 BitDepth = 32
)

// BytesPerSample returns the number of bytes needed to store one sample.
// Sub-byte depths (4-bit) are stored in 1 byte (sign-extended).
// 12-bit samples are stored in 2 bytes (sign-extended to 16-bit).
// 20-bit samples are stored in 3 bytes (sign-extended to 24-bit).
func (d BitDepth) BytesPerSample() int {
	switch d {
	case Depth4, Depth8:
		return 1
	case Depth12, Depth16:
		return 2
	case Depth20, Depth24:
		return 3
	case Depth32:
		return 4
	default:
		panic(fmt.Sprintf("flac: BytesPerSample called with unsupported bit depth %d", d))
	}
}

// PCMFormat describes the format of raw PCM audio data.
type PCMFormat struct {
	SampleRate int
	BitDepth   BitDepth
	Channels   uint
}
