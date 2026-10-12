package search

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"testing"
	"time"
)

// naive is the obviously-correct reference: every match start, in order.
func naive(data []byte, q Query) []int64 {
	hay, needle := data, q.Needle
	if q.Fold {
		hay, needle = bytes.ToLower(data), bytes.ToLower(needle)
	}
	var out []int64
	for i := 0; i+len(needle) <= len(hay); i++ {
		if bytes.Equal(hay[i:i+len(needle)], needle) {
			out = append(out, int64(i))
		}
	}
	return out
}

// Random small files and needles, with chunks tiny enough that matches
// straddle chunk boundaries often. Find and Count must agree with naive.
func TestAgainstNaive(t *testing.T) {
	defer func(c int64) { chunkSize = c }(chunkSize)
	rng := rand.New(rand.NewSource(1))
	alphabet := []byte("aAbB,1\n")
	ctx := context.Background()

	for iter := 0; iter < 3000; iter++ {
		chunkSize = int64(1 + rng.Intn(8))
		data := make([]byte, rng.Intn(60))
		for i := range data {
			data[i] = alphabet[rng.Intn(len(alphabet))]
		}
		needle := make([]byte, 1+rng.Intn(3))
		for i := range needle {
			needle[i] = alphabet[rng.Intn(len(alphabet))]
		}
		q := Query{Needle: needle, Fold: rng.Intn(2) == 0}
		r, size := bytes.NewReader(data), int64(len(data))
		want := naive(data, q)

		if n, err := Count(ctx, r, size, q); err != nil || n != int64(len(want)) {
			t.Fatalf("Count(%q in %q, fold=%v) = %d, %v; want %d", needle, data, q.Fold, n, err, len(want))
		}

		from := int64(rng.Intn(len(data) + 1))
		wantNext, wantPrev := int64(-1), int64(-1)
		if len(want) > 0 {
			wantNext, wantPrev = want[0], want[len(want)-1] // wrapped around
		}
		for _, m := range want {
			if m >= from {
				wantNext = m
				break
			}
		}
		for _, m := range want {
			if m < from {
				wantPrev = m
			}
		}
		if got, _ := Find(ctx, r, size, q, from, false); got != wantNext {
			t.Fatalf("next %q in %q from %d, fold=%v: got %d, want %d", needle, data, from, q.Fold, got, wantNext)
		}
		if got, _ := Find(ctx, r, size, q, from, true); got != wantPrev {
			t.Fatalf("prev %q in %q from %d, fold=%v: got %d, want %d", needle, data, from, q.Fold, got, wantPrev)
		}
	}
}

// Case-insensitive applies to A-Z only; other bytes, such as UTF-8 letters
// with accents, must match exactly.
func TestFoldNonASCII(t *testing.T) {
	data := []byte("Éclair, éclair, ÉCLAIR")
	n, _ := Count(context.Background(), bytes.NewReader(data), int64(len(data)), Query{Needle: []byte("Éclair"), Fold: true})
	if n != 2 { // "Éclair" and "ÉCLAIR", not "éclair"
		t.Errorf("got %d matches, want 2", n)
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := bytes.Repeat([]byte("abc\n"), 1000)
	if _, err := Count(ctx, bytes.NewReader(data), int64(len(data)), Query{Needle: []byte("abc")}); err == nil {
		t.Error("Count should report the cancelled context")
	}
}

// Timing on a real file: TXT_BENCH_FILE=~/Desktop/trades-4gb.csv go test -run Timing -v ./internal/search
func TestTiming(t *testing.T) {
	path := os.Getenv("TXT_BENCH_FILE")
	if path == "" {
		t.Skip("set TXT_BENCH_FILE to time searches on a real file")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	ctx := context.Background()
	for _, q := range []Query{{[]byte("RELIANCE"), false}, {[]byte("reliance"), true}, {[]byte("NEEDLEZZ"), false}, {[]byte("needlezz"), true}, {[]byte("CL12345"), true}} {
		start := time.Now()
		n, _ := Count(ctx, f, info.Size(), q)
		countTime := time.Since(start)
		start = time.Now()
		at, _ := Find(ctx, f, info.Size(), q, info.Size()/2, false)
		t.Logf("%-9s fold=%-5v count %9d in %v; next from middle at %d in %v",
			q.Needle, q.Fold, n, countTime.Round(time.Millisecond), at, time.Since(start).Round(time.Microsecond))
	}
}

// LineAt and Total must agree with counting newlines directly, with blocks
// and chunks small enough that every boundary case comes up.
func TestLineIndex(t *testing.T) {
	defer func(c, b int64) { chunkSize, blockSize = c, b }(chunkSize, blockSize)
	rng := rand.New(rand.NewSource(2))
	for iter := 0; iter < 2000; iter++ {
		blockSize = int64(1 + rng.Intn(4))
		chunkSize = blockSize * int64(1+rng.Intn(3))
		data := make([]byte, rng.Intn(40))
		for i := range data {
			data[i] = "ab\n"[rng.Intn(3)]
		}
		ix, err := IndexLines(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		want := int64(bytes.Count(data, []byte{'\n'}))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			want++
		}
		if ix.Total != want {
			t.Fatalf("Total of %q = %d, want %d", data, ix.Total, want)
		}
		for off := 0; off <= len(data); off++ {
			got, err := ix.LineAt(bytes.NewReader(data), int64(off))
			if want := int64(bytes.Count(data[:off], []byte{'\n'})) + 1; err != nil || got != want {
				t.Fatalf("LineAt(%q, %d) = %d, %v; want %d", data, off, got, err, want)
			}
		}
	}
}
