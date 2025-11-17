package field

import "encoding/binary"

// GF128 represents GF(2^128) as 8-dimensional vector over GF16
type GF128 [8]GF16

// Zero returns the zero element in GF128
func Zero() GF128 {
	return GF128{}
}

// Add128 adds two GF128 elements (component-wise XOR)
func Add128(a, b GF128) GF128 {
	var result GF128
	for i := 0; i < 8; i++ {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// Mul128 multiplies a GF16 scalar with a GF128 vector
func Mul128(scalar GF16, vec GF128) GF128 {
	var result GF128
	for i := 0; i < 8; i++ {
		result[i] = Mul16(scalar, vec[i])
	}
	return result
}

// MulAdd128 performs fused multiply-add: result += scalar * vec
// This is faster than separate Mul128 + Add128 as it avoids intermediate allocation
// Unrolled for better performance (8 GF16 elements is fixed)
func MulAdd128(result *GF128, scalar GF16, vec GF128) {
	result[0] ^= Mul16(scalar, vec[0])
	result[1] ^= Mul16(scalar, vec[1])
	result[2] ^= Mul16(scalar, vec[2])
	result[3] ^= Mul16(scalar, vec[3])
	result[4] ^= Mul16(scalar, vec[4])
	result[5] ^= Mul16(scalar, vec[5])
	result[6] ^= Mul16(scalar, vec[6])
	result[7] ^= Mul16(scalar, vec[7])
}

// ToBytes128 serializes a GF128 to 16 bytes (little-endian)
func ToBytes128(g GF128) [16]byte {
	var b [16]byte
	ToBytes128InPlace(g, b[:])
	return b
}

// ToBytes128InPlace serializes a GF128 into a provided 16-byte buffer (little-endian)
// Panics if the buffer is not exactly 16 bytes
func ToBytes128InPlace(g GF128, buf []byte) {
	if len(buf) != 16 {
		panic("ToBytes128InPlace requires exactly 16-byte buffer")
	}
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(g[i]))
	}
}

// FromBytes128 deserializes 16 bytes to a GF128 (little-endian)
func FromBytes128(b [16]byte) GF128 {
	var g GF128
	for i := 0; i < 8; i++ {
		g[i] = GF16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return g
}

// Equal checks if two GF128 values are equal
func Equal128(a, b GF128) bool {
	for i := 0; i < 8; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HashToGF128 converts a 32-byte hash to a GF128 element
// XORs the two halves for better randomness distribution
func HashToGF128(data []byte) GF128 {
	if len(data) < 32 {
		panic("HashToGF128 requires at least 32 bytes")
	}

	// Take first half as 8 little-endian uint16 values
	var firstHalf GF128
	for i := 0; i < 8; i++ {
		firstHalf[i] = GF16(binary.LittleEndian.Uint16(data[i*2:]))
	}

	// Take second half as 8 little-endian uint16 values
	var secondHalf GF128
	for i := 0; i < 8; i++ {
		secondHalf[i] = GF16(binary.LittleEndian.Uint16(data[16+i*2:]))
	}

	// XOR the two halves for final result
	return Add128(firstHalf, secondHalf)
}
