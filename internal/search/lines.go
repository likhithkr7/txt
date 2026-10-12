package search

import (
	"bytes"
	"context"
	"io"
)

// blockSize is the line index's granularity: finding the line at an offset
// counts newlines in at most one block. chunkSize must be a multiple of it.
var blockSize int64 = 64 << 10

// LineIndex numbers the lines of a file without keeping them: for every
// block it records how many newlines come before it (512 KB for 4 GB).
type LineIndex struct {
	before []int64 // newlines in [0, i*blockSize)
	size   int64
	Total  int64 // lines in the file; a last line without "\n" counts
}

// IndexLines scans the whole file once, one goroutine per core.
func IndexLines(ctx context.Context, r io.ReaderAt, size int64) (*LineIndex, error) {
	counts := make([]int64, (size+blockSize-1)/blockSize)
	all := spans(0, size)
	err := run(ctx, r, size, Query{Needle: []byte{'\n'}}, len(all), func(k int) (span, bool) {
		return all[k], true
	}, func(_ int, sp span, b []byte) {
		for i := int64(0); i < sp.end-sp.start; i += blockSize {
			counts[(sp.start+i)/blockSize] = int64(bytes.Count(b[i:min(i+blockSize, sp.end-sp.start)], []byte{'\n'}))
		}
	})
	if err != nil {
		return nil, err
	}
	ix := &LineIndex{before: make([]int64, len(counts)+1), size: size}
	for i, n := range counts {
		ix.before[i+1] = ix.before[i] + n
	}
	ix.Total = ix.before[len(counts)]
	if size > 0 {
		last := make([]byte, 1)
		if _, err := r.ReadAt(last, size-1); err != nil {
			return nil, err
		}
		if last[0] != '\n' {
			ix.Total++
		}
	}
	return ix, nil
}

// LineAt returns the number (from 1) of the line containing byte offset off.
func (ix *LineIndex) LineAt(r io.ReaderAt, off int64) (int64, error) {
	off = max(0, min(off, ix.size))
	b := min(off/blockSize, int64(len(ix.before)-1))
	buf := make([]byte, off-b*blockSize)
	if n, err := r.ReadAt(buf, b*blockSize); n < len(buf) {
		return 0, err
	}
	return ix.before[b] + int64(bytes.Count(buf, []byte{'\n'})) + 1, nil
}
