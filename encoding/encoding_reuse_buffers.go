package encoding

import (
	"fmt"

	"github.com/celestiaorg/reedsolomon"
	"github.com/celestiaorg/rsema1d/field"
)

// ExtendVerticalWithReuseBuffers performs vertical RS extension using Leopard GF16
// with provided reuseBuffers to avoid allocating shard buffers.
func ExtendVerticalWithReuseBuffers(data [][]byte, n int, shards [][]byte) ([][]byte, error) {
	k := len(data)
	if k == 0 {
		return nil, fmt.Errorf("no data provided")
	}
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive, got %d", n)
	}

	// Check that all rows have the same size
	rowSize := len(data[0])
	if rowSize == 0 || rowSize%64 != 0 {
		return nil, fmt.Errorf("row size must be non-zero and multiple of 64, got %d", rowSize)
	}
	for i, row := range data {
		if len(row) != rowSize {
			return nil, fmt.Errorf("row %d has size %d, expected %d", i, len(row), rowSize)
		}
	}

	// Validate reuseBuffers size
	if len(shards) != k+n {
		return nil, fmt.Errorf("reuseBuffers shards must have size k+n=%d, got %d", k+n, len(shards))
	}
	for i, shard := range shards {
		if len(shard) < rowSize {
			return nil, fmt.Errorf("reuseBuffers shard %d has size %d, need at least %d", i, len(shard), rowSize)
		}
	}

	// Create Reed-Solomon encoder
	enc, err := reedsolomon.New(k, n, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	// Sub-slice shards to exactly rowSize bytes (capacity is preserved)
	for i := range shards {
		shards[i] = shards[i][:rowSize]
	}

	// Copy data rows into reuseBuffers
	for i := 0; i < k; i++ {
		copy(shards[i], data[i])
	}

	// Clear parity shards
	for i := k; i < k+n; i++ {
		for j := range shards[i] {
			shards[i][j] = 0
		}
	}

	// Encode to generate parity shards
	if err := enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}

	// Return sub-slices (original + parity)
	return shards, nil
}

// ExtendRLCResultsWithReuseBuffers extends RLC results using Reed-Solomon
// with provided reuseBuffers to avoid allocating shard buffers.
func ExtendRLCResultsWithReuseBuffers(rlcOriginal []field.GF128, n int, rlcShards [][]byte) ([]field.GF128, error) {
	k := len(rlcOriginal)
	if k == 0 {
		return nil, fmt.Errorf("no RLC values provided")
	}
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive, got %d", n)
	}

	// Validate reuseBuffers size
	if len(rlcShards) != k {
		return nil, fmt.Errorf("reuseBuffers rlcShards must have size k=%d, got %d", k, len(rlcShards))
	}
	for i, shard := range rlcShards {
		if len(shard) != 64 {
			return nil, fmt.Errorf("reuseBuffers rlcShard %d has size %d, expected 64", i, len(shard))
		}
	}

	// Pack GF128 values into reuseBuffers shards
	for i := 0; i < k; i++ {
		packGF128ToLeopardInPlace(rlcOriginal[i], rlcShards[i])
	}

	// Extend using vertical RS (this will allocate parity shards internally,
	// but that's much smaller - only K*64 bytes vs (K+N)*rowSize bytes)
	extendedShards, err := ExtendVertical(rlcShards, n)
	if err != nil {
		return nil, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// Extract GF128 values from extended Leopard shards
	extended := make([]field.GF128, k+n)
	for i := 0; i < k+n; i++ {
		extended[i] = unpackGF128FromLeopard(extendedShards[i])
	}

	return extended, nil
}

// packGF128ToLeopardInPlace packs a GF128 value into a pre-allocated 64-byte slice
func packGF128ToLeopardInPlace(g field.GF128, shard []byte) {
	if len(shard) != 64 {
		panic("packGF128ToLeopardInPlace requires exactly 64-byte shard")
	}

	// Pack 8 GF16 symbols in Leopard interleaved format
	for i := 0; i < 8; i++ {
		symbol := g[i]
		shard[i] = byte(symbol & 0xFF)
		shard[32+i] = byte(symbol >> 8)
	}
	// Zero out positions 8-31 and 40-63
	for i := 8; i < 32; i++ {
		shard[i] = 0
		shard[32+i] = 0
	}
}

// ReconstructWithReuseBuffers recovers original data from any K rows
// using provided reuseBuffers to avoid allocating shard buffers.
func ReconstructWithReuseBuffers(rows [][]byte, indices []int, k, n int, shards [][]byte) ([][]byte, error) {
	if len(rows) != len(indices) {
		return nil, fmt.Errorf("rows and indices must have same length: %d != %d", len(rows), len(indices))
	}

	if len(rows) < k {
		return nil, fmt.Errorf("need at least %d rows, got %d", k, len(rows))
	}

	if k <= 0 {
		return nil, fmt.Errorf("k must be positive, got %d", k)
	}

	if n <= 0 {
		return nil, fmt.Errorf("n must be positive, got %d", n)
	}

	// Validate reuseBuffers size
	if len(shards) != k+n {
		return nil, fmt.Errorf("reuseBuffers shards must have size k+n=%d, got %d", k+n, len(shards))
	}

	// Validate indices are in range
	for _, idx := range indices {
		if idx < 0 || idx >= k+n {
			return nil, fmt.Errorf("index %d out of range [0, %d)", idx, k+n)
		}
	}

	// Create Reed-Solomon decoder
	enc, err := reedsolomon.New(k, n, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	// Clear reuseBuffers
	for i := range shards {
		shards[i] = nil
	}

	// Place available rows in their positions
	for i, idx := range indices {
		shards[idx] = rows[i]
	}

	// Reconstruct missing shards
	if err := enc.Reconstruct(shards); err != nil {
		return nil, fmt.Errorf("failed to reconstruct: %w", err)
	}

	// Return only the original k rows
	original := make([][]byte, k)
	for i := 0; i < k; i++ {
		original[i] = shards[i]
	}

	return original, nil
}
