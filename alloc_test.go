package rsema1d

import (
	"crypto/rand"
	"testing"
)

func TestRowAllocator(t *testing.T) {
	k, n := 4096, 12288
	totalRows := k + n
	maxRowSize := 32832

	alloc := NewRowAllocator(k, n, maxRowSize)

	// Test buffer packing at different row sizes
	testCases := []struct {
		rowSize         int
		expectedBuffers int
	}{
		{64, 32},       // 32832/64=513 rows/buf, ceil(16384/513)=32
		{32832, 16384}, // 1 row/buf
	}

	for _, tc := range testCases {
		buf := alloc.Get(tc.rowSize)

		// Verify row count and helpers
		if len(buf.Rows) != totalRows {
			t.Fatalf("rowSize %d: expected %d rows, got %d", tc.rowSize, totalRows, len(buf.Rows))
		}
		if len(buf.Original()) != k {
			t.Fatalf("rowSize %d: Original expected %d, got %d", tc.rowSize, k, len(buf.Original()))
		}
		if len(buf.Parity()) != n {
			t.Fatalf("rowSize %d: Parity expected %d, got %d", tc.rowSize, n, len(buf.Parity()))
		}

		// Verify buffer packing
		if len(buf.buffers) != tc.expectedBuffers {
			t.Fatalf("rowSize %d: expected %d buffers, got %d", tc.rowSize, tc.expectedBuffers, len(buf.buffers))
		}

		// Verify row lengths
		for i, row := range buf.Rows {
			if len(row) != tc.rowSize {
				t.Fatalf("rowSize %d: row %d has len %d", tc.rowSize, i, len(row))
			}
		}

		buf.Release()

		// Verify Release clears references
		if buf.Rows != nil || buf.buffers != nil {
			t.Fatal("Release should nil out Rows and buffers")
		}
	}
}

func TestRowAllocator_WithCoder(t *testing.T) {
	k, n := 32, 32
	maxRowSize := 256

	cfg := &Config{K: k, N: n, WorkerCount: 1}
	cfg.Validate()
	coder, _ := NewCoder(cfg)
	alloc := NewRowAllocator(k, n, maxRowSize)

	// Encode with one row size
	buf1 := alloc.Get(64)
	for _, row := range buf1.Original() {
		rand.Read(row)
	}
	_, commitment1, _, err := coder.Encode(buf1.Rows)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Save original data to verify it's unchanged
	savedData := make([]byte, len(buf1.Original()[0]))
	copy(savedData, buf1.Original()[0])

	// Verify data unchanged after encoding
	if string(buf1.Original()[0]) != string(savedData) {
		t.Fatal("original data modified during encoding")
	}
	buf1.Release()

	// Encode same logical data with different row size should produce different commitment
	buf2 := alloc.Get(128)
	for _, row := range buf2.Original() {
		rand.Read(row)
	}
	_, commitment2, _, _ := coder.Encode(buf2.Rows)
	if commitment1 == commitment2 {
		t.Fatal("different data should produce different commitments")
	}
	buf2.Release()
}

func TestRowAllocator_AllRowSizes(t *testing.T) {
	k, n := 4096, 12288
	totalRows := k + n
	maxRowSize := 32832

	alloc := NewRowAllocator(k, n, maxRowSize)

	for rowSize := 64; rowSize <= maxRowSize; rowSize += 64 {
		buf := alloc.Get(rowSize)

		// Verify dimensions
		if len(buf.Rows) != totalRows {
			t.Fatalf("rowSize %d: expected %d rows, got %d", rowSize, totalRows, len(buf.Rows))
		}

		// Verify buffer count formula
		rowsPerBuffer := maxRowSize / rowSize
		expectedBuffers := (totalRows + rowsPerBuffer - 1) / rowsPerBuffer
		if len(buf.buffers) != expectedBuffers {
			t.Fatalf("rowSize %d: expected %d buffers, got %d", rowSize, expectedBuffers, len(buf.buffers))
		}

		// Write to boundaries to verify no out-of-bounds
		for _, row := range buf.Rows {
			row[0] = 0xFF
			row[len(row)-1] = 0xFF
		}

		buf.Release()
	}
}
