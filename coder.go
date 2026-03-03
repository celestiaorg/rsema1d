package rsema1d

import (
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/reedsolomon"
	"github.com/celestiaorg/rsema1d/field"
)

// Coder provides encoding and reconstruction operations with a cached Reed-Solomon encoder.
// Use NewCoder to create an instance and reuse it for multiple operations with the same Config.
type Coder struct {
	config *Config
	enc    reedsolomon.Encoder
}

// NewCoder creates a Coder with cached Reed-Solomon encoder.
func NewCoder(cfg *Config) (*Coder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	enc, err := reedsolomon.New(cfg.K, cfg.N, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	return &Coder{config: cfg, enc: enc}, nil
}

// Encode creates parity (if missing) and commitment for K+N rows.
// rows must have length K+N. Original data goes in rows[:K].
// Parity rows (rows[K:]) can be nil (will be generated) or already filled
// (e.g., after reconstruction - will be left unchanged).
func (c *Coder) Encode(rows [][]byte) (*ExtendedData, Commitment, []field.GF128, error) {
	if len(rows) != c.config.K+c.config.N {
		return nil, Commitment{}, nil, fmt.Errorf("expected %d rows, got %d", c.config.K+c.config.N, len(rows))
	}

	// check if this is fresh encoding (all parity allocated and zeroed) or reconstruction
	// use Encode for fresh data (much faster), Reconstruct only when needed
	freshEncode := isZero(rows[c.config.K:])
	if freshEncode {
		if err := c.enc.Encode(rows); err != nil {
			return nil, Commitment{}, nil, fmt.Errorf("failed to encode: %w", err)
		}
	} else {
		// reconstruction: some rows may be nil/missing, use Reconstruct
		if err := c.enc.Reconstruct(rows); err != nil {
			return nil, Commitment{}, nil, fmt.Errorf("failed to reconstruct: %w", err)
		}
	}

	rowSize := len(rows[0])

	// build padded Merkle tree for rows
	rowTree := buildPaddedRowTree(rows, c.config)
	rowRoot := rowTree.Root()

	// derive RLC coefficients and compute RLC results for original rows
	coeffs := deriveCoefficients(rowRoot, rowSize)
	rlcOrig := computeRLCOrig(rows[:c.config.K], coeffs, c.config)

	// build padded RLC Merkle tree
	rlcOrigTree := buildPaddedRLCTree(rlcOrig, c.config)
	rlcOrigRoot := rlcOrigTree.Root()

	// create commitment: SHA256(rowRoot || rlcOrigRoot)
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcOrigRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	extData := &ExtendedData{
		config:      c.config,
		rows:        rows,
		rowRoot:     rowRoot,
		rlcOrig:     rlcOrig,
		rowTree:     rowTree,
		rlcOrigTree: rlcOrigTree,
		rlcOrigRoot: rlcOrigRoot,
	}

	return extData, commitment, rlcOrig, nil
}

// isZero checks if all bytes in the given rows are zero.
func isZero(rows [][]byte) bool {
	for _, row := range rows {
		if row == nil {
			return false
		}
		for _, v := range row {
			if v != 0 {
				return false
			}
		}
	}
	return true
}
