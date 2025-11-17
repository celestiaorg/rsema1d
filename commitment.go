package rsema1d

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/celestiaorg/rsema1d/field"
)

// deriveCoefficients generates RLC coefficients via Fiat-Shamir (internal)
func deriveCoefficients(rowRoot [32]byte, config *Config) []field.GF128 {
	seed := sha256.Sum256(rowRoot[:])
	numSymbols := config.RowSize / 2 // Each GF16 symbol is 2 bytes
	coeffs := make([]field.GF128, numSymbols)

	var input [32 + 4]byte
	copy(input[:32], seed[:])
	for i := 0; i < numSymbols; i++ {
		binary.LittleEndian.PutUint32(input[32:], uint32(i))
		digest := sha256.Sum256(input[:])
		coeffs[i] = field.HashToGF128(digest[:])
	}
	return coeffs
}

// computeRLC computes random linear combination for a row (internal)
// Optimized version that inlines symbol extraction and uses fused multiply-add
func computeRLC(row []byte, coeffs []field.GF128) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize

	for c := 0; c < numChunks; c++ {
		chunkOffset := c * chunkSize
		chunk := row[chunkOffset : chunkOffset+chunkSize]
		baseSymbolIndex := c * 32

		// Process 32 symbols per chunk (64 bytes = 32 GF16 symbols)
		// Unroll by 4 to improve instruction-level parallelism and reduce loop overhead
		for j := 0; j < 32; j += 4 {
			// Extract and process 4 symbols at once
			// This helps with CPU pipelining and reduces branch mispredictions
			s0 := field.GF16(chunk[32+j+0])<<8 | field.GF16(chunk[j+0])
			s1 := field.GF16(chunk[32+j+1])<<8 | field.GF16(chunk[j+1])
			s2 := field.GF16(chunk[32+j+2])<<8 | field.GF16(chunk[j+2])
			s3 := field.GF16(chunk[32+j+3])<<8 | field.GF16(chunk[j+3])

			// Fused multiply-add for each symbol
			field.MulAdd128(&result, s0, coeffs[baseSymbolIndex+j+0])
			field.MulAdd128(&result, s1, coeffs[baseSymbolIndex+j+1])
			field.MulAdd128(&result, s2, coeffs[baseSymbolIndex+j+2])
			field.MulAdd128(&result, s3, coeffs[baseSymbolIndex+j+3])
		}
	}
	return result
}

// extractSymbols extracts GF16 symbols from Leopard-formatted chunk (internal)
// Implements Appendix A.1 from spec
func extractSymbols(chunk []byte) []field.GF16 {
	if len(chunk) != chunkSize {
		panic("extractSymbols requires exactly 64-byte chunk")
	}

	symbols := make([]field.GF16, 32)
	for i := 0; i < 32; i++ {
		// Leopard format: bytes 0-31 are low bytes, 32-63 are high bytes
		symbols[i] = field.GF16(chunk[32+i])<<8 | field.GF16(chunk[i])
	}
	return symbols
}
