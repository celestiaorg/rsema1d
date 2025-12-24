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

	// internal buffers for RLC operations (lazily allocated)
	rlcShards  [][]byte
	rlcResults []field.GF128
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

	// validate against config.RowSize if specified
	if c.config.RowSize > 0 && len(rows) > 0 && len(rows[0]) != c.config.RowSize {
		return nil, Commitment{}, nil, fmt.Errorf("row size %d does not match config %d", len(rows[0]), c.config.RowSize)
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

// CreateVerificationContext initializes a verification context with RLC original values.
// Reuses the Coder's internal buffers for extending RLC results.
//
// The returned VerificationContext references internal buffers and is only valid
// until the next call to CreateVerificationContext on the same Coder.
// For concurrent use, create separate Coder instances per goroutine.
func (c *Coder) CreateVerificationContext(rlcOrig []field.GF128) (*VerificationContext, [32]byte, error) {
	rlcExtended, err := c.extendRLCResults(rlcOrig)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	rlcOrigTree := buildPaddedRLCTree(rlcOrig, c.config)
	rlcOrigRoot := rlcOrigTree.Root()

	return &VerificationContext{
		config:      c.config,
		rlcOrig:     rlcOrig,
		rlcExtended: rlcExtended,
		rlcOrigRoot: rlcOrigRoot,
	}, rlcOrigRoot, nil
}

// extendRLCResults extends k RLC values to k+n values using Reed-Solomon.
// returns internal buffer - valid until next call.
func (c *Coder) extendRLCResults(rlcOrig []field.GF128) ([]field.GF128, error) {
	if len(rlcOrig) != c.config.K {
		return nil, fmt.Errorf("expected %d RLC values, got %d", c.config.K, len(rlcOrig))
	}

	c.ensureRLCBuffers()

	// pack GF128 values into Leopard-formatted rows
	for i := range c.config.K {
		field.PackToLeopard(rlcOrig[i], c.rlcShards[i])
	}
	// clear parity rows
	for i := c.config.K; i < c.config.K+c.config.N; i++ {
		clear(c.rlcShards[i])
	}

	if err := c.enc.Encode(c.rlcShards); err != nil {
		return nil, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// unpack to cached result buffer
	for i := range c.config.K + c.config.N {
		c.rlcResults[i] = field.UnpackFromLeopard(c.rlcShards[i])
	}

	return c.rlcResults, nil
}

// ensureRLCBuffers lazily allocates internal buffers for RLC operations.
func (c *Coder) ensureRLCBuffers() {
	if c.rlcShards != nil {
		return
	}
	c.rlcShards = make([][]byte, c.config.K+c.config.N)
	for i := range c.rlcShards {
		c.rlcShards[i] = make([]byte, 64)
	}
	c.rlcResults = make([]field.GF128, c.config.K+c.config.N)
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
