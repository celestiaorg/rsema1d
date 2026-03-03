package rsema1d

import "sync"

// RowAllocator manages pooled row buffers for encoding operations.
//
// Encoding operations require K+N row buffers, where each row has the same size.
// Row sizes vary based on blob size (must be multiples of 64, ranging from 64 to maxRowSize).
// Instead of allocating new buffers for each operation, RowAllocator maintains a pool
// of individual maxRowSize buffers that are packed with multiple rows when rowSize < maxRowSize.
//
// This approach:
//   - Eliminates per-operation allocations for the common case (buffer reuse from pool)
//   - Memory efficient: small blobs pack multiple rows per buffer
//   - For smallest rows (64 bytes), only ~32 buffers needed instead of 16384
//   - For largest rows (maxRowSize), one row per buffer (16384 buffers)
//
// Memory usage examples (K=4096, N=12288, maxRowSize=32832):
//
//	| Row Size | Rows/Buffer | Buffers | Memory        |
//	|----------|-------------|---------|---------------|
//	| 64       | 513         | 32      | ~1 MiB        |
//	| 256      | 128         | 128     | ~4 MiB        |
//	| 1024     | 32          | 512     | ~16 MiB       |
//	| 4096     | 8           | 2048    | ~64 MiB       |
//	| 32832    | 1           | 16384   | ~512 MiB      |
//
// Usage:
//
//	alloc := NewRowAllocator(k, n, maxRowSize)
//
//	// For each blob:
//	buf := alloc.Get(rowSize)
//	copy(buf.Original(), data)        // fill K original rows
//	extData, _, _, _ := coder.Encode(buf.Rows)
//	// ... use extData ...
//	buf.Release()                     // return to pool for reuse
//
// RowAllocator is safe for concurrent use. Each Get returns an independent buffer.
// However, individual RowBuffer instances are not safe for concurrent use.
type RowAllocator struct {
	k, n       int
	maxRowSize int
	pool       sync.Pool // pools individual []byte of maxRowSize
}

// NewRowAllocator creates an allocator for K+N rows with given max row size.
// The maxRowSize should be the largest row size that will ever be requested,
// typically computed from the maximum blob size supported by the system.
func NewRowAllocator(k, n, maxRowSize int) *RowAllocator {
	a := &RowAllocator{
		k:          k,
		n:          n,
		maxRowSize: maxRowSize,
	}
	a.pool.New = func() any {
		return make([]byte, maxRowSize)
	}
	return a
}

// RowBuffer is a pooled buffer of K+N rows obtained from a RowAllocator.
// The Rows slice contains K+N byte slices, each with length equal to the
// requested rowSize. Multiple rows may share the same underlying buffer
// when rowSize < maxRowSize.
//
// After use, call Release to return the buffer to the pool. The buffer
// must not be used after Release is called.
type RowBuffer struct {
	alloc   *RowAllocator
	Rows    [][]byte // K+N rows, subsliced views into buffers
	buffers [][]byte // underlying physical buffers to return to pool
}

// Get returns a buffer with K+N rows of the specified rowSize.
// The rowSize must be <= maxRowSize and should be a multiple of 64.
// Multiple rows are packed into each physical buffer when rowSize < maxRowSize.
func (a *RowAllocator) Get(rowSize int) *RowBuffer {
	totalRows := a.k + a.n
	rowsPerBuffer := a.maxRowSize / rowSize
	numBuffers := (totalRows + rowsPerBuffer - 1) / rowsPerBuffer // ceil division

	buffers := make([][]byte, numBuffers)
	rows := make([][]byte, totalRows)

	rowIdx := 0
	for i := range numBuffers {
		buf := a.pool.Get().([]byte)
		buffers[i] = buf

		// Pack rows into this buffer
		for j := 0; j < rowsPerBuffer && rowIdx < totalRows; j++ {
			offset := j * rowSize
			rows[rowIdx] = buf[offset : offset+rowSize]
			rowIdx++
		}
	}

	return &RowBuffer{alloc: a, Rows: rows, buffers: buffers}
}

// Release returns the underlying buffers to the pool.
// The buffer must not be used after calling Release.
func (b *RowBuffer) Release() {
	for _, buf := range b.buffers {
		b.alloc.pool.Put(buf)
	}
	b.buffers = nil
	b.Rows = nil
}

// Original returns the first K rows.
func (b *RowBuffer) Original() [][]byte {
	return b.Rows[:b.alloc.k]
}

// Parity returns the last N rows.
func (b *RowBuffer) Parity() [][]byte {
	return b.Rows[b.alloc.k:]
}
