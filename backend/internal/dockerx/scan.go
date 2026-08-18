package dockerx

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// maxLogLine bounds a single line so a container emitting a gigabyte without a
// newline cannot exhaust the dashboard's memory.
const maxLogLine = 256 * 1024

func scanLines(ctx context.Context, r io.Reader, stream string, out chan<- LogLine) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLogLine)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		select {
		case <-ctx.Done():
			return
		case out <- LogLine{Stream: stream, Text: line}:
		}
	}
}
