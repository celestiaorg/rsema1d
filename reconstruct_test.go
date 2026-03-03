package rsema1d

import (
	"bytes"
	"testing"
)

func TestReconstructionWithProofs(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := makeTestConfig(tc)
			config.RowSize = 0 // variable row size for Coder
			originalData := makeTestData(tc.k, tc.rowSize)

			coder, err := NewCoder(config)
			if err != nil {
				t.Fatalf("NewCoder() error: %v", err)
			}

			// prepare rows with zeroed parity
			rows := make([][]byte, tc.k+tc.n)
			for i := range tc.k {
				rows[i] = make([]byte, tc.rowSize)
				copy(rows[i], originalData[i])
			}
			for i := tc.k; i < tc.k+tc.n; i++ {
				rows[i] = make([]byte, tc.rowSize)
			}

			extData, commitment, _, err := coder.Encode(rows)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			tests := []struct {
				name    string
				indices []int
			}{
				{"original_rows", makeRange(0, tc.k)},
				{"parity_rows", makeRange(tc.k, tc.k+tc.n)[:tc.k]},
				{"mixed_rows", makeMixedIndices(tc.k, tc.n)},
			}

			for _, rt := range tests {
				t.Run(rt.name, func(t *testing.T) {
					recon := NewReconstruction(coder, commitment)

					// set rows with proofs
					for _, idx := range rt.indices {
						proof, err := extData.GenerateRowInclusionProof(idx)
						if err != nil {
							t.Fatalf("GenerateRowInclusionProof(%d) error: %v", idx, err)
						}
						if err := recon.SetRow(proof); err != nil {
							t.Fatalf("SetRow(%d) error: %v", idx, err)
						}
					}

					// finish reconstruction
					reconExtData, _, err := recon.Finish()
					if err != nil {
						t.Fatalf("Finish() error: %v", err)
					}

					// verify original data matches
					for i := range tc.k {
						if !bytes.Equal(recon.Original()[i], originalData[i]) {
							t.Errorf("Original row %d mismatch", i)
						}
					}

					// verify all extended rows match
					for i := range tc.k + tc.n {
						if !bytes.Equal(reconExtData.rows[i], extData.rows[i]) {
							t.Errorf("Extended row %d mismatch", i)
						}
					}
				})
			}
		})
	}
}

func TestReconstructionRejectsInvalidProof(t *testing.T) {
	config := &Config{K: 4, N: 4, WorkerCount: 1}
	originalData := makeTestData(4, 64)

	coder, _ := NewCoder(config)

	rows := make([][]byte, 8)
	for i := range 4 {
		rows[i] = make([]byte, 64)
		copy(rows[i], originalData[i])
	}
	for i := 4; i < 8; i++ {
		rows[i] = make([]byte, 64)
	}

	extData, commitment, _, _ := coder.Encode(rows)

	// create reconstruction with correct commitment
	recon := NewReconstruction(coder, commitment)

	// get a valid proof and corrupt it
	proof, _ := extData.GenerateRowInclusionProof(0)
	proof.Row[0] ^= 0xFF

	// should reject corrupted proof
	if err := recon.SetRow(proof); err == nil {
		t.Error("SetRow() should reject corrupted proof")
	}
}
