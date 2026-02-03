# Saprobe FLAC

Pure Go FLAC streaming decoder and encoder.

Thin layer over a [fork](https://github.com/mycophonic/flac) of [mewkiz/flac](https://github.com/mewkiz/flac),
providing our standard PCM API.

An example cli decoder is provided.

For a full-blown cli, see [Saprobe](https://github.com/mycophonic/saprobe).

## Support

- **Bit depths:** 4, 8, 12, 16, 20, 24, 32
- **Channels:** 1-8 (mono through 7.1 surround)
- **Sample rates:** any valid uint32; tested at 8000-192000 Hz (11 rates)
- **Output:** interleaved little-endian signed PCM

| Bit Depth | Bytes/Sample | Notes      |
|-----------|--------------|------------|
| 4         | 1            | Signed LE  |
| 8         | 1            | Signed LE  |
| 12        | 2            | Signed LE  |
| 16        | 2            | Signed LE  |
| 20        | 3            | Signed LE  |
| 24        | 3            | Signed LE  |
| 32        | 4            | Signed LE  |

## API

```go
func NewDecoder(rs io.ReadSeeker) (*Decoder, error)
func (d *Decoder) Read(p []byte) (int, error)
func (d *Decoder) Format() PCMFormat
func (d *Decoder) Close() error

func Decode(rs io.ReadSeeker) ([]byte, PCMFormat, error)
func Encode(writer io.Writer, pcm []byte, format PCMFormat) error
```

## Dependencies

[github.com/mewkiz/flac](https://github.com/mewkiz/flac) (via [mycophonic fork](https://github.com/mycophonic/flac))

Other dependencies (agar) are purely for test tooling.

## Detailed documentation

* [FLAC format notes](./docs/FLAC.md)
* [decoders landscape](./docs/research/DECODERS.md)
* [encoders landscape](./docs/research/ENCODERS.md)
