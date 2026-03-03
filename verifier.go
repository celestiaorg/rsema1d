package rsema1d

import (
	"fmt"

	"github.com/celestiaorg/reedsolomon"
	"github.com/celestiaorg/rsema1d/field"
)

// Verifier provides row verification operations with a cached Reed-Solomon encoder.
type Verifier struct {
	config *Config
	enc    reedsolomon.Encoder

	// internal buffers for RLC operations
	rlcShards  [][]byte
	rlcResults []field.GF128
}

// NewVerifier creates a Verifier with cached Reed-Solomon encoder.
func NewVerifier(cfg *Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	enc, err := reedsolomon.New(cfg.K, cfg.N, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	rlcShards := make([][]byte, cfg.K+cfg.N)
	for i := range rlcShards {
		rlcShards[i] = make([]byte, 64)
	}

	return &Verifier{
		config:     cfg,
		enc:        enc,
		rlcShards:  rlcShards,
		rlcResults: make([]field.GF128, cfg.K+cfg.N),
	}, nil
}

// CreateVerificationContext initializes a verification context with RLC original values.
// Reuses the Verifier's internal buffers for extending RLC results.
//
// The returned VerificationContext references internal buffers and is only valid
// until the next call to CreateVerificationContext on the same Verifier.
// For concurrent use, create separate Verifier instances per goroutine.
func (v *Verifier) CreateVerificationContext(rlcOrig []field.GF128) (*VerificationContext, [32]byte, error) {
	rlcExtended, err := v.extendRLC(rlcOrig)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	rlcOrigTree := buildPaddedRLCTree(rlcOrig, v.config)
	rlcOrigRoot := rlcOrigTree.Root()

	return &VerificationContext{
		config:      v.config,
		rlcOrig:     rlcOrig,
		rlcExtended: rlcExtended,
		rlcOrigRoot: rlcOrigRoot,
	}, rlcOrigRoot, nil
}

// extendRLC extends k RLC values to k+n values using Reed-Solomon.
// returns internal buffer - valid until next call.
func (v *Verifier) extendRLC(rlcOrig []field.GF128) ([]field.GF128, error) {
	if len(rlcOrig) != v.config.K {
		return nil, fmt.Errorf("expected %d RLC values, got %d", v.config.K, len(rlcOrig))
	}

	// pack GF128 values into Leopard-formatted rows
	for i := range v.config.K {
		field.PackToLeopard(rlcOrig[i], v.rlcShards[i])
	}
	// clear parity rows
	for i := v.config.K; i < v.config.K+v.config.N; i++ {
		clear(v.rlcShards[i])
	}

	if err := v.enc.Encode(v.rlcShards); err != nil {
		return nil, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// unpack to cached result buffer
	for i := range v.config.K + v.config.N {
		v.rlcResults[i] = field.UnpackFromLeopard(v.rlcShards[i])
	}

	return v.rlcResults, nil
}
