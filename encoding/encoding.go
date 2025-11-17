package encoding

import (
	"fmt"

	"github.com/celestiaorg/reedsolomon"
	"github.com/celestiaorg/rsema1d/field"
)

// ExtendVertical performs vertical RS extension using Leopard GF16
func ExtendVertical(data [][]byte, n int) ([][]byte, error) {
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

	// Create Reed-Solomon encoder
	// Always use Leopard GF16 for consistency, even for small shard counts
	enc, err := reedsolomon.New(k, n, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	// Create shards array with data and space for parity
	shards := make([][]byte, k+n)

	// Copy data rows
	for i := 0; i < k; i++ {
		shards[i] = make([]byte, rowSize)
		copy(shards[i], data[i])
	}

	// Create empty parity shards
	for i := k; i < k+n; i++ {
		shards[i] = make([]byte, rowSize)
	}

	// Encode to generate parity shards
	if err := enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}

	// Return all shards (original + parity)
	return shards, nil
}

// extendVerticalInPlace performs RS extension using pre-allocated shards buffer
// shards must already have k+n slices allocated, with first k containing data
// This is a zero-allocation variant of ExtendVertical for the hot path
func extendVerticalInPlace(shards [][]byte, k, n int) ([][]byte, error) {
	if len(shards) != k+n {
		return nil, fmt.Errorf("shards length %d doesn't match k+n=%d", len(shards), k+n)
	}
	if k == 0 {
		return nil, fmt.Errorf("no data provided")
	}
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive, got %d", n)
	}

	// Check that all rows have the same size
	rowSize := len(shards[0])
	if rowSize == 0 || rowSize%64 != 0 {
		return nil, fmt.Errorf("row size must be non-zero and multiple of 64, got %d", rowSize)
	}
	for i := 0; i < k+n; i++ {
		if len(shards[i]) != rowSize {
			return nil, fmt.Errorf("shard %d has size %d, expected %d", i, len(shards[i]), rowSize)
		}
	}

	// Create Reed-Solomon encoder
	enc, err := reedsolomon.New(k, n, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	// Encode to generate parity shards (modifies shards[k:k+n] in place)
	if err := enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}

	return shards, nil
}

// packGF128ToLeopard packs a GF128 value into a 64-byte Leopard-formatted shard
// The GF128 consists of 8 GF16 symbols, placed as the first 8 symbols of the chunk
// with the remaining 24 symbol positions zero-padded
func packGF128ToLeopard(g field.GF128) []byte {
	shard := make([]byte, 64)
	packGF128ToLeopardInPlace(g, shard)
	return shard
}

// packGF128ToLeopardInPlace packs a GF128 value into an existing 64-byte buffer
// Buffer must be pre-zeroed or this function will zero the unused positions
func packGF128ToLeopardInPlace(g field.GF128, shard []byte) {
	if len(shard) != 64 {
		panic("packGF128ToLeopardInPlace requires exactly 64-byte shard")
	}

	// Pack 8 GF16 symbols in Leopard interleaved format
	// Symbols 0-7 from GF128, symbols 8-31 are zero
	for i := 0; i < 8; i++ {
		symbol := g[i]
		shard[i] = byte(symbol & 0xFF)  // Low byte at position i
		shard[32+i] = byte(symbol >> 8) // High byte at position 32+i
	}
	// Positions 8-31 and 40-63 should already be zero (caller's responsibility)
}

// unpackGF128FromLeopard extracts a GF128 value from a 64-byte Leopard-formatted shard
// It reads the first 8 GF16 symbols from the Leopard chunk
func unpackGF128FromLeopard(shard []byte) field.GF128 {
	if len(shard) != 64 {
		panic("unpackGF128FromLeopard requires exactly 64-byte shard")
	}

	var g field.GF128
	// Extract first 8 GF16 symbols from Leopard interleaved format
	for i := 0; i < 8; i++ {
		lowByte := shard[i]
		highByte := shard[32+i]
		g[i] = field.GF16(highByte)<<8 | field.GF16(lowByte)
	}

	return g
}

// ExtendRLCResults extends RLC results using Reed-Solomon
// If shardBuffer is provided and large enough ((k+n)*64 bytes), it will be used
// for zero-allocation operation. Otherwise, standard allocation is used.
func ExtendRLCResults(rlcOriginal []field.GF128, n int, shardBuffer []byte) ([]field.GF128, error) {
	k := len(rlcOriginal)
	if k == 0 {
		return nil, fmt.Errorf("no RLC values provided")
	}
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive, got %d", n)
	}

	requiredBufferSize := (k + n) * 64
	useProvidedBuffer := len(shardBuffer) >= requiredBufferSize

	// Prepare shards slice
	shards := make([][]byte, k+n)

	if useProvidedBuffer {
		// Zero-allocation path: slice the provided buffer
		for i := 0; i < k+n; i++ {
			shards[i] = shardBuffer[i*64 : (i+1)*64]
			// Zero out the buffer
			for j := range shards[i] {
				shards[i][j] = 0
			}
		}
	}

	// Pack original GF128 values into shards
	for i := 0; i < k; i++ {
		if useProvidedBuffer {
			packGF128ToLeopardInPlace(rlcOriginal[i], shards[i])
		} else {
			shards[i] = packGF128ToLeopard(rlcOriginal[i])
		}
	}

	// Extend using vertical RS
	var extendedShards [][]byte
	var err error
	if useProvidedBuffer {
		// Zero-allocation: extend in-place (shards already has space for k+n)
		extendedShards, err = extendVerticalInPlace(shards, k, n)
	} else {
		extendedShards, err = ExtendVertical(shards[:k], n)
	}

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

// Reconstruct recovers original data from any K rows
// k parameter is necessary here as we need to know how many original rows to reconstruct
func Reconstruct(rows [][]byte, indices []int, k, n int) ([][]byte, error) {
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

	// Validate indices are in range
	for _, idx := range indices {
		if idx < 0 || idx >= k+n {
			return nil, fmt.Errorf("index %d out of range [0, %d)", idx, k+n)
		}
	}

	// Create Reed-Solomon decoder with same parameters as encoder
	// Always use Leopard GF16 for consistency
	enc, err := reedsolomon.New(k, n, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	// Create shards array with nils for missing data
	shards := make([][]byte, k+n)

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
