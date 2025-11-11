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

// computeRLCFromRowRoot derives coefficients on the fly directly from the
// row root. This avoids allocating the entire coefficient slice, which is only
// needed when verifying a single row.
func computeRLCFromRowRoot(row []byte, rowRoot [32]byte, config *Config) field.GF128 {
	seed := sha256.Sum256(rowRoot[:])
	return computeRLCWithSeed(row, seed, config)
}

func computeRLCWithSeed(row []byte, seed [32]byte, config *Config) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize
	var input [32 + 4]byte
	copy(input[:32], seed[:])
	var symbols [32]field.GF16
	var symbolIndex uint32

	for c := 0; c < numChunks; c++ {
		chunk := row[c*chunkSize : (c+1)*chunkSize]
		extractSymbolsInto(&symbols, chunk)
		for j := 0; j < len(symbols); j++ {
			binary.LittleEndian.PutUint32(input[32:], symbolIndex)
			digest := sha256.Sum256(input[:])
			coeff := field.HashToGF128(digest[:])
			product := field.Mul128(symbols[j], coeff)
			result = field.Add128(result, product)
			symbolIndex++
		}
	}
	return result
}

// computeRLC computes random linear combination for a row (internal)
func computeRLC(row []byte, coeffs []field.GF128, config *Config) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize
	var symbols [32]field.GF16
	symbolIndex := 0

	for c := 0; c < numChunks; c++ {
		chunk := row[c*chunkSize : (c+1)*chunkSize]
		extractSymbolsInto(&symbols, chunk)
		for j := 0; j < len(symbols); j++ {
			// result += symbol * coefficient
			product := field.Mul128(symbols[j], coeffs[symbolIndex])
			result = field.Add128(result, product)
			symbolIndex++ // Overall symbol index in the row
		}
	}
	return result
}

// extractSymbols extracts GF16 symbols from Leopard-formatted chunk (internal)
// Implements Appendix A.1 from spec
func extractSymbols(chunk []byte) []field.GF16 {
	var symbols [32]field.GF16
	extractSymbolsInto(&symbols, chunk)
	result := make([]field.GF16, len(symbols))
	copy(result, symbols[:])
	return result
}

func extractSymbolsInto(dst *[32]field.GF16, chunk []byte) {
	if len(chunk) != chunkSize {
		panic("extractSymbols requires exactly 64-byte chunk")
	}
	for i := 0; i < len(dst); i++ {
		// Leopard format: bytes 0-31 are low bytes, 32-63 are high bytes
		dst[i] = field.GF16(chunk[32+i])<<8 | field.GF16(chunk[i])
	}
}
