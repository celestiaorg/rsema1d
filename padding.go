package rsema1d

import (
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)

// buildPaddedRowTree creates a padded Merkle tree from extended rows
func buildPaddedRowTree(extended [][]byte, config *Config) *merkle.Tree {
	zeroRow := make([]byte, config.RowSize)
	paddedRows := make([][]byte, config.totalPadded)

	// Fill padded array: [original | padding | extended | padding]
	copy(paddedRows[0:config.K], extended[0:config.K]) // Original rows
	for i := config.K; i < config.kPadded; i++ {
		paddedRows[i] = zeroRow // Padding after K
	}
	copy(paddedRows[config.kPadded:config.kPadded+config.N], extended[config.K:]) // Extended rows
	for i := config.kPadded + config.N; i < config.totalPadded; i++ {
		paddedRows[i] = zeroRow // Padding at end
	}

	return merkle.NewTreeWithWorkers(paddedRows, config.WorkerCount)
}

// buildPaddedRLCTree creates a padded Merkle tree from RLC original values
// Only stores K values padded to kPadded (not totalPadded like row tree)
// If config.RLCLeavesBuffer is provided and large enough (kPadded*16 bytes),
// it will be used for zero-allocation operation.
func buildPaddedRLCTree(rlcOrig []field.GF128, config *Config) *merkle.Tree {
	requiredBufferSize := config.kPadded * 16
	useProvidedBuffer := len(config.RLCLeavesBuffer) >= requiredBufferSize

	paddedRLCLeaves := make([][]byte, config.kPadded)

	if useProvidedBuffer {
		// Zero-allocation path: slice the provided buffer
		buf := config.RLCLeavesBuffer[:requiredBufferSize]

		// Zero out the entire buffer (for padding)
		for i := range buf {
			buf[i] = 0
		}

		// Slice buffer into 16-byte chunks and fill with K original RLC values
		for i := 0; i < config.K; i++ {
			chunk := buf[i*16 : (i+1)*16]
			field.ToBytes128InPlace(rlcOrig[i], chunk)
			paddedRLCLeaves[i] = chunk
		}

		// Point padding entries to zero-filled portions of buffer
		for i := config.K; i < config.kPadded; i++ {
			paddedRLCLeaves[i] = buf[i*16 : (i+1)*16]
		}
	} else {
		// Standard allocation path
		zeroRLC := make([]byte, 16) // Zero GF128 value

		// Fill with K original RLC values
		for i := 0; i < config.K; i++ {
			bytes := field.ToBytes128(rlcOrig[i])
			paddedRLCLeaves[i] = bytes[:]
		}
		// Pad to next power of 2
		for i := config.K; i < config.kPadded; i++ {
			paddedRLCLeaves[i] = zeroRLC
		}
	}

	return merkle.NewTreeWithWorkers(paddedRLCLeaves, config.WorkerCount)
}

// mapIndexToTreePosition maps an actual row index to its position in the padded tree
func mapIndexToTreePosition(index int, config *Config) int {
	if index < config.K {
		return index // Original rows stay at same position
	}
	return config.kPadded + (index - config.K) // Extended rows shifted by padding
}
