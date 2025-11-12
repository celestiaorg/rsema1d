package rsema1d

import (
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
)

// EncodeWithReuseBuffers extends data vertically and creates commitment using provided reuseBuffers.
func EncodeWithReuseBuffers(data [][]byte, config *Config, reuseBuffers *EncoderReuseBuffers) (*ExtendedData, Commitment, []field.GF128, error) {
	if err := config.Validate(); err != nil {
		return nil, Commitment{}, nil, fmt.Errorf("invalid config: %w", err)
	}

	if reuseBuffers == nil {
		return nil, Commitment{}, nil, fmt.Errorf("reuseBuffers cannot be nil")
	}

	// Allow buffers with larger capacity (K+N) - more flexible for varying blob sizes
	bufferTotalRows := reuseBuffers.config.K + reuseBuffers.config.N
	actualTotalRows := config.K + config.N
	if bufferTotalRows < actualTotalRows {
		return nil, Commitment{}, nil, fmt.Errorf("reuseBuffers too small: buffer K+N=%d < required K+N=%d", bufferTotalRows, actualTotalRows)
	}

	// Allow buffers with RowSize >= actual row size (can use larger buffers for smaller rows)
	if reuseBuffers.config.RowSize < config.RowSize {
		return nil, Commitment{}, nil, fmt.Errorf("reuseBuffers too small: buffer RowSize=%d < required RowSize=%d", reuseBuffers.config.RowSize, config.RowSize)
	}

	if len(data) != config.K {
		return nil, Commitment{}, nil, fmt.Errorf("expected %d rows, got %d", config.K, len(data))
	}

	for i, row := range data {
		if len(row) != config.RowSize {
			return nil, Commitment{}, nil, fmt.Errorf("row %d has size %d, expected %d", i, len(row), config.RowSize)
		}
	}

	extended, err := encoding.ExtendVerticalWithReuseBuffers(data, config.N, reuseBuffers.shards)
	if err != nil {
		return nil, Commitment{}, nil, fmt.Errorf("failed to extend data: %w", err)
	}

	rowTree := buildPaddedRowTree(extended, config)
	rowRoot := rowTree.Root()

	coeffs := deriveCoefficients(rowRoot, config)
	rlcOrig := computeRLCOrig(data, coeffs, config)

	rlcExtended, err := encoding.ExtendRLCResultsWithReuseBuffers(rlcOrig, config.N, reuseBuffers.rlcShards)
	if err != nil {
		return nil, Commitment{}, nil, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	rlcTree := buildPaddedRLCTree(rlcExtended, config)
	rlcRoot := rlcTree.Root()

	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	extData := &ExtendedData{
		config:  config,
		rows:    extended,
		rowRoot: rowRoot,
		rlcRoot: rlcRoot,
		rlcOrig: rlcOrig,
		rowTree: rowTree,
		rlcTree: rlcTree,
	}

	return extData, commitment, rlcOrig, nil
}

// VerifyRowWithReuseBuffers verifies a row proof using pre-initialized context and reuseBuffers.
func VerifyRowWithReuseBuffers(proof *RowProof, commitment Commitment, context *VerificationContext, reuseBuffers *VerifierReuseBuffers) error {
	if proof == nil || context == nil {
		return fmt.Errorf("received nil proof or context in verifier")
	}

	if reuseBuffers == nil {
		return fmt.Errorf("reuseBuffers cannot be nil")
	}

	// Allow buffers with larger capacity (K+N) - more flexible for varying blob sizes
	bufferTotalRows := reuseBuffers.config.K + reuseBuffers.config.N
	actualTotalRows := context.config.K + context.config.N
	if bufferTotalRows < actualTotalRows {
		return fmt.Errorf("reuseBuffers too small: buffer K+N=%d < required K+N=%d", bufferTotalRows, actualTotalRows)
	}

	// Allow buffers with RowSize >= actual row size (can use larger buffers for smaller rows)
	if reuseBuffers.config.RowSize < context.config.RowSize {
		return fmt.Errorf("reuseBuffers too small: buffer RowSize=%d < required RowSize=%d", reuseBuffers.config.RowSize, context.config.RowSize)
	}

	if proof.Index < 0 || proof.Index >= context.config.K+context.config.N {
		return fmt.Errorf("index %d out of range [0, %d)", proof.Index, context.config.K+context.config.N)
	}

	if len(proof.Row) != context.config.RowSize {
		return fmt.Errorf("row size mismatch: expected %d, got %d", context.config.RowSize, len(proof.Row))
	}

	treeIndex := mapIndexToTreePosition(proof.Index, context.config)
	rowRoot, err := computeRootFromProofWithReuseBuffers(
		proof.Row,
		treeIndex,
		proof.RowProof,
		reuseBuffers.merkleProofBuf[:len(proof.RowProof)],
		reuseBuffers.leafHashBuf,
		reuseBuffers.innerHashBuf,
	)
	if err != nil {
		return fmt.Errorf("failed to compute row root: %w", err)
	}

	computedRLC := computeRLCFromRowRoot(proof.Row, rowRoot, context.config)

	if proof.Index >= len(context.rlcExtended) {
		return fmt.Errorf("index %d out of range", proof.Index)
	}

	expectedRLC := context.rlcExtended[proof.Index]
	if !field.Equal128(computedRLC, expectedRLC) {
		return fmt.Errorf("computed RLC does not match expected value")
	}

	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(context.rlcRoot[:])
	computedCommitment := h.Sum(nil)

	if commitment != [32]byte(computedCommitment) {
		return fmt.Errorf("commitment verification failed")
	}

	return nil
}

// computeRootFromProofWithReuseBuffers computes Merkle root using reuseBuffers buffers
func computeRootFromProofWithReuseBuffers(
	leaf []byte,
	leafIndex int,
	proof [][]byte,
	proofBuf [][]byte,
	leafHashBuf []byte,
	innerHashBuf []byte,
) ([32]byte, error) {
	// Allow proofBuf to be larger than proof (use only what we need)
	if len(proofBuf) < len(proof) {
		return [32]byte{}, fmt.Errorf("proof buffer too small: has %d slots, need %d", len(proofBuf), len(proof))
	}
	for i := range proof {
		copy(proofBuf[i], proof[i])
	}

	// Compute leaf hash using reuseBuffers buffer
	leafHashBuf = leafHashBuf[:0]
	leafHashBuf = append(leafHashBuf, 0) // Leaf prefix
	leafHashBuf = append(leafHashBuf, leaf...)
	current := sha256.Sum256(leafHashBuf)

	// Traverse up the tree (only use the proof length, not full proofBuf)
	index := leafIndex
	for i := range proof {
		sibling := proofBuf[i]
		innerHashBuf = innerHashBuf[:0]
		innerHashBuf = append(innerHashBuf, 1) // Inner node prefix

		if index%2 == 0 {
			// Current is left child
			innerHashBuf = append(innerHashBuf, current[:]...)
			innerHashBuf = append(innerHashBuf, sibling...)
		} else {
			// Current is right child
			innerHashBuf = append(innerHashBuf, sibling...)
			innerHashBuf = append(innerHashBuf, current[:]...)
		}

		current = sha256.Sum256(innerHashBuf)
		index /= 2
	}

	return current, nil
}

// ReconstructWithReuseBuffers recovers original data from any K rows using reuseBuffers.
func ReconstructWithReuseBuffers(rows [][]byte, indices []int, config *Config, reuseBuffers *EncoderReuseBuffers) ([][]byte, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if reuseBuffers == nil {
		return nil, fmt.Errorf("reuseBuffers cannot be nil")
	}

	// Allow buffers with larger capacity (K+N) - more flexible for varying blob sizes
	bufferTotalRows := reuseBuffers.config.K + reuseBuffers.config.N
	actualTotalRows := config.K + config.N
	if bufferTotalRows < actualTotalRows {
		return nil, fmt.Errorf("reuseBuffers too small: buffer K+N=%d < required K+N=%d", bufferTotalRows, actualTotalRows)
	}

	// Allow buffers with RowSize >= actual row size (can use larger buffers for smaller rows)
	if reuseBuffers.config.RowSize < config.RowSize {
		return nil, fmt.Errorf("reuseBuffers too small: buffer RowSize=%d < required RowSize=%d", reuseBuffers.config.RowSize, config.RowSize)
	}

	return encoding.ReconstructWithReuseBuffers(rows, indices, config.K, config.N, reuseBuffers.shards)
}
