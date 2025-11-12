package rsema1d

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/celestiaorg/rsema1d/field"
)

// deriveCoefficients generates RLC coefficients via Fiat-Shamir (internal)
func deriveCoefficients(rowRoot [32]byte, config *Config) []field.GF128 {
	h := sha256.New()
	h.Write(rowRoot[:])

	var buf [12]byte
	binary.LittleEndian.PutUint32(buf[0:4], uint32(config.K))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(config.N))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(config.RowSize))
	h.Write(buf[:])
	seed := h.Sum(nil)

	numSymbols := config.RowSize / 2
	coeffs := make([]field.GF128, numSymbols)

	var input [32 + 4]byte
	copy(input[:32], seed)
	for i := 0; i < numSymbols; i++ {
		binary.LittleEndian.PutUint32(input[32:], uint32(i))
		digest := sha256.Sum256(input[:])
		coeffs[i] = field.HashToGF128(digest[:])
	}
	return coeffs
}

// computeRLC computes random linear combination for a row (internal)
func computeRLC(row []byte, coeffs []field.GF128) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize

	for c := 0; c < numChunks; c++ {
		chunk := row[c*chunkSize : (c+1)*chunkSize]
		symbols := extractSymbols(chunk)
		for j, sym := range symbols {
			// result += symbol * coefficient
			symbolIndex := c*32 + j // Overall symbol index in the row
			product := field.Mul128(sym, coeffs[symbolIndex])
			result = field.Add128(result, product)
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
