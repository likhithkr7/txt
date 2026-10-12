// Package search finds plain text in files of any size without loading
// them: the file is read in chunks by one goroutine per CPU core.
package search

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
)

// chunkSize is how much each goroutine reads at a time. Small enough that
// the nearest match is found quickly; a var so tests can shrink it.
var chunkSize int64 = 4 << 20

// Query is what to look for. With Fold, ASCII letters match either case
// (other bytes must match exactly).
type Query struct {
	Needle []byte
	Fold   bool
}

// span is a chunk of the file: matches must start in [start, end), but may
// run on past end.
type span struct{ start, end int64 }

func spans(from, to int64) []span {
	var out []span
	for s := from; s < to; s += chunkSize {
		out = append(out, span{s, min(s+chunkSize, to)})
	}
	return out
}

func reversed(s []span) []span {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// Find returns the offset of the first match at or after from, wrapping
// around to the start of the file; with backward, the last match before
// from, wrapping around to the end. It returns -1 if there is no match.
func Find(ctx context.Context, r io.ReaderAt, size int64, q Query, from int64, backward bool) (int64, error) {
	from = max(0, min(from, size))
	var order []span
	if backward {
		order = append(reversed(spans(0, from)), reversed(spans(from, size))...)
	} else {
		order = append(spans(from, size), spans(0, from)...)
	}

	// Workers take spans in order; the first span in `order` that has a match
	// wins. Spans after a known winner are skipped, but every span before it
	// is still searched, so the answer is always the nearest match.
	found := make([]int64, len(order))
	var best atomic.Int64
	best.Store(int64(len(order)))
	err := run(ctx, r, size, q, len(order), func(k int) (span, bool) {
		return order[k], int64(k) < best.Load()
	}, func(k int, sp span, b []byte) {
		at := int64(-1)
		forEach(b, sp.end-sp.start, q, func(i int) bool {
			at = sp.start + int64(i)
			return backward // backward wants the last match in the span, forward the first
		})
		if at < 0 {
			return
		}
		found[k] = at
		for cur := best.Load(); int64(k) < cur && !best.CompareAndSwap(cur, int64(k)); cur = best.Load() {
		}
	})
	if err != nil {
		return -1, err
	}
	if k := best.Load(); k < int64(len(order)) {
		return found[k], nil
	}
	return -1, nil
}

// Count returns how many times the needle occurs in the file, counting
// overlapping matches ("aa" occurs 3 times in "aaaa").
func Count(ctx context.Context, r io.ReaderAt, size int64, q Query) (int64, error) {
	all := spans(0, size)
	var total atomic.Int64
	err := run(ctx, r, size, q, len(all), func(k int) (span, bool) {
		return all[k], true
	}, func(_ int, sp span, b []byte) {
		n := int64(0)
		forEach(b, sp.end-sp.start, q, func(int) bool { n++; return true })
		total.Add(n)
	})
	return total.Load(), err
}

// run hands spans 0..n-1, in order, to one goroutine per core. take says
// whether span k is still wanted; visit gets its bytes plus enough of the
// next span for a match that starts inside it.
func run(ctx context.Context, r io.ReaderAt, size int64, q Query, n int,
	take func(k int) (span, bool), visit func(k int, sp span, b []byte)) error {
	if len(q.Needle) == 0 {
		return nil
	}
	overlap := int64(len(q.Needle) - 1)
	var next atomic.Int64
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	for range min(runtime.NumCPU(), n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, chunkSize+overlap)
			for {
				k := int(next.Add(1) - 1)
				if k >= n || ctx.Err() != nil {
					return
				}
				sp, ok := take(k)
				if !ok {
					return
				}
				got, err := r.ReadAt(buf[:min(sp.end+overlap, size)-sp.start], sp.start)
				if err != nil && err != io.EOF {
					errOnce.Do(func() { firstErr = err })
					return
				}
				visit(k, sp, buf[:got])
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// forEach calls fn with the index of each match in b that starts before
// limit, in order, until fn returns false.
func forEach(b []byte, limit int64, q Query, fn func(i int) bool) {
	needle := q.Needle
	if !q.Fold {
		for pos := 0; ; {
			i := bytes.Index(b[pos:], needle)
			if i < 0 || int64(pos+i) >= limit || !fn(pos+i) {
				return
			}
			pos += i + 1
		}
	}
	if q.Fold {
		needle = lowerASCII(needle) // only A-Z, exactly as matchAt folds the file's bytes
	}
	// Jump between occurrences of one byte of the needle (the anchor), then
	// compare the whole needle there. The anchor is the needle byte that's
	// rarest in a sample of b (counting both cases of a letter), so it stops
	// as few times as possible: "CL12345" anchors on L, not on a digit.
	var freq [256]int
	for _, c := range b[:min(len(b), 64<<10)] {
		freq[c]++
	}
	anchor, best := 0, -1
	for i, c := range needle {
		n := freq[c]
		if isLower(c) {
			n += freq[c-'a'+'A']
		}
		if best == -1 || n < best {
			anchor, best = i, n
		}
	}
	a := needle[anchor]
	lowerA, upperA := a, a
	if q.Fold && isLower(a) {
		upperA = a - 'a' + 'A'
	}

	// Next occurrence of each case of the anchor at or after pos (or -1)
	nextLower, nextUpper := -2, -2
	find := func(c byte, pos int) int {
		if i := bytes.IndexByte(b[pos:], c); i >= 0 {
			return pos + i
		}
		return -1
	}
	for pos := anchor; pos < len(b); {
		if nextLower != -1 && nextLower < pos {
			nextLower = find(lowerA, pos)
		}
		if upperA != lowerA && nextUpper != -1 && nextUpper < pos {
			nextUpper = find(upperA, pos)
		}
		at := nextLower
		if upperA != lowerA && (at == -1 || (nextUpper != -1 && nextUpper < at)) {
			at = nextUpper
		}
		if at == -1 {
			return
		}
		start := at - anchor
		if int64(start) >= limit {
			return
		}
		if start+len(needle) <= len(b) && matchAt(b[start:start+len(needle)], needle, q.Fold) {
			if !fn(start) {
				return
			}
		}
		pos = at + 1
	}
}

func lowerASCII(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

func matchAt(b, needle []byte, fold bool) bool {
	if !fold {
		return bytes.Equal(b, needle)
	}
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != needle[i] {
			return false
		}
	}
	return true
}
