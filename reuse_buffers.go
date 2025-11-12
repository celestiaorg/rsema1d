package rsema1d

import (
	"fmt"
	"runtime"

	"github.com/celestiaorg/rsema1d/field"
)

// VerifierReuseBuffers provides reusable buffers for verification operations.
type VerifierReuseBuffers struct {
	config *Config

	coeffsBuf []field.GF128

	merkleProofBuf [][]byte // Reused for proof verification
	leafHashBuf    []byte   // For computing leaf hashes
	innerHashBuf   []byte   // For computing internal node hashes
}

// NewVerifierReuseBuffers creates new reuse buffers for verification operations.
func NewVerifierReuseBuffers(config *Config) (*VerifierReuseBuffers, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	numSymbols := config.RowSize / 2 // Each GF16 symbol is 2 bytes

	// Calculate tree depth for merkle proof buffer
	kPadded := nextPowerOfTwo(config.K)
	totalPadded := nextPowerOfTwo(kPadded + config.N)
	treeDepth := 0
	for n := totalPadded; n > 1; n >>= 1 {
		treeDepth++
	}

	// Pre-allocate merkle proof buffer slices
	merkleProofBuf := make([][]byte, treeDepth)
	for i := 0; i < treeDepth; i++ {
		merkleProofBuf[i] = make([]byte, 32)
	}

	return &VerifierReuseBuffers{
		config:         config,
		coeffsBuf:      make([]field.GF128, numSymbols),
		merkleProofBuf: merkleProofBuf,
		leafHashBuf:    make([]byte, 0, config.RowSize+1), // +1 for prefix
		innerHashBuf:   make([]byte, 0, 65),               // prefix + 2*32 bytes
	}, nil
}

// Reset clears the buffers in the reuseBuffers (zeroes them out).
func (w *VerifierReuseBuffers) Reset() {
	// Clear coefficient buffer
	for i := range w.coeffsBuf {
		w.coeffsBuf[i] = field.Zero()
	}

	// Clear merkle buffers
	for i := range w.merkleProofBuf {
		for j := range w.merkleProofBuf[i] {
			w.merkleProofBuf[i][j] = 0
		}
	}

	w.leafHashBuf = w.leafHashBuf[:0]
	w.innerHashBuf = w.innerHashBuf[:0]
}

// NewVerifierReuseBuffersPool creates a pool of verifier reuse buffers for concurrent operations.
func NewVerifierReuseBuffersPool(config *Config, poolSize int) (*VerifierReuseBuffersPool, error) {
	if poolSize < 1 {
		poolSize = runtime.NumCPU()
	}

	pool := &VerifierReuseBuffersPool{
		config:  config,
		buffers: make(chan *VerifierReuseBuffers, poolSize),
	}

	// Pre-create all buffers
	for i := 0; i < poolSize; i++ {
		buf, err := NewVerifierReuseBuffers(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create verifier buffer %d: %w", i, err)
		}
		pool.buffers <- buf
	}

	return pool, nil
}

// VerifierReuseBuffersPool manages a pool of verifier reuse buffers for concurrent operations.
// This allows multiple goroutines to safely reuse buffers without contention during parallel verification.
type VerifierReuseBuffersPool struct {
	config  *Config
	buffers chan *VerifierReuseBuffers
}

// Get retrieves a buffer from the pool. Blocks if none are available.
func (p *VerifierReuseBuffersPool) Get() *VerifierReuseBuffers {
	return <-p.buffers
}

// Put returns a buffer to the pool for reuse.
func (p *VerifierReuseBuffersPool) Put(buf *VerifierReuseBuffers) {
	p.buffers <- buf
}

// Size returns the pool size.
func (p *VerifierReuseBuffersPool) Size() int {
	return cap(p.buffers)
}

// EncoderReuseBuffers provides reusable buffers for encoding operations.
type EncoderReuseBuffers struct {
	config *Config

	shards    [][]byte
	rlcShards [][]byte
}

// NewEncoderReuseBuffers creates new reuse buffers for encoding operations.
func NewEncoderReuseBuffers(config *Config) (*EncoderReuseBuffers, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Pre-allocate main shard buffers
	totalShards := config.K + config.N
	shards := make([][]byte, totalShards)
	for i := 0; i < totalShards; i++ {
		shards[i] = make([]byte, config.RowSize)
	}

	// Pre-allocate RLC shard buffers (64 bytes each for GF128 packing)
	rlcShards := make([][]byte, config.K)
	for i := 0; i < config.K; i++ {
		rlcShards[i] = make([]byte, 64)
	}

	return &EncoderReuseBuffers{
		config:    config,
		shards:    shards,
		rlcShards: rlcShards,
	}, nil
}

// Reset clears the buffers in the reuseBuffers (zeroes them out).
func (w *EncoderReuseBuffers) Reset() {
	// Clear shard buffers
	for i := range w.shards {
		for j := range w.shards[i] {
			w.shards[i][j] = 0
		}
	}

	// Clear RLC shard buffers
	for i := range w.rlcShards {
		for j := range w.rlcShards[i] {
			w.rlcShards[i][j] = 0
		}
	}
}
