package memoryintegration

import (
	"bufio"
	"bytes"
	"context"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
)

const MaxFrameBytes = 1024 * 1024

// Bounds are applied before SDK JSON decoding. One UTF-8 JSON-RPC message must
// occupy one newline-terminated frame; oversized/malformed frames close input.
type frameReader struct {
	source  io.ReadCloser
	scanner *bufio.Scanner
	pending []byte
}

func (r *frameReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return 0, memory.ErrInvalidInput
			}
			return 0, io.EOF
		}
		token := r.scanner.Bytes()
		if memory.ValidateCanonicalJSON(token) != nil {
			return 0, memory.ErrInvalidInput
		}
		r.pending = append(append([]byte(nil), token...), '\n')
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
func (r *frameReader) Close() error { return r.source.Close() }
func splitFrame(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return 0, nil, memory.ErrInvalidInput
	}
	return 0, nil, nil
}

type frameWriter struct{ io.Writer }

func (w frameWriter) Write(p []byte) (int, error) {
	if len(p) > MaxFrameBytes {
		return 0, memory.ErrInvalidInput
	}
	return w.Writer.Write(p)
}
func (frameWriter) Close() error { return nil }
func Serve(ctx context.Context, g *memory.Gateway, c SessionConfig, r io.ReadCloser, w io.Writer) error {
	s, err := NewServer(g, c)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes)
	scanner.Split(splitFrame)
	err = s.Run(ctx, &mcp.IOTransport{Reader: &frameReader{source: r, scanner: scanner}, Writer: frameWriter{w}})
	if err != nil && ctx.Err() != nil {
		return memory.ErrCanceled
	}
	if err != nil {
		return memory.ErrUnavailable
	}
	return nil
}
