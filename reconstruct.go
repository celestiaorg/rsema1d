package rsema1d

import (
	"errors"

	"github.com/celestiaorg/rsema1d/field"
)

// ErrCommitmentMismatch is returned when reconstructed data doesn't match expected commitment.
var ErrCommitmentMismatch = errors.New("reconstructed commitment does not match expected")

// Reconstruction accumulates verified rows for erasure decoding.
type Reconstruction struct {
	coder      *Coder
	commitment Commitment
	rows       [][]byte // len k+n, nil for missing
}

// NewReconstruction creates a Reconstruction for streaming row accumulation.
// Each row is verified against the commitment before being accepted.
func NewReconstruction(coder *Coder, commitment Commitment) *Reconstruction {
	return &Reconstruction{
		coder:      coder,
		commitment: commitment,
		rows:       make([][]byte, coder.config.K+coder.config.N),
	}
}

// SetRow verifies the row inclusion proof against the commitment and stores the row.
// The row slice is retained by reference (not copied).
func (r *Reconstruction) SetRow(proof *RowInclusionProof) error {
	if err := VerifyRowInclusionProof(proof, r.commitment, r.coder.config); err != nil {
		return err
	}
	r.rows[proof.Index] = proof.Row
	return nil
}

// Commitment returns the expected commitment being reconstructed.
func (r *Reconstruction) Commitment() Commitment {
	return r.commitment
}

// Finish reconstructs, re-encodes, and verifies commitment.
// Returns ExtendedData and RLC coefficients on success.
// Returns ErrCommitmentMismatch if reconstructed commitment doesn't match expected.
func (r *Reconstruction) Finish() (*ExtendedData, []field.GF128, error) {
	extData, commitment, rlcCoeffs, err := r.coder.Encode(r.rows)
	if err != nil {
		return nil, nil, err
	}
	if commitment != r.commitment {
		return nil, nil, ErrCommitmentMismatch
	}
	return extData, rlcCoeffs, nil
}

// Original returns the first K rows (original data rows).
// Only valid after Finish has been called successfully.
func (r *Reconstruction) Original() [][]byte {
	return r.rows[:r.coder.config.K]
}

// Extended returns all K+N rows.
// Only valid after Finish has been called successfully.
func (r *Reconstruction) Extended() [][]byte {
	return r.rows
}
