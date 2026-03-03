package rsema1d

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/reedsolomon"
)

func BenchmarkEncoderCreation(b *testing.B) {
	sizes := []struct {
		name string
		k, n int
	}{
		{"4x4", 4, 4},
		{"64x64", 64, 64},
		{"1024x1024", 1024, 1024},
		{"4096x12288", 4096, 12288},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			for b.Loop() {
				_, err := reedsolomon.New(sz.k, sz.n, reedsolomon.WithLeopardGF16(true))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCoderEncode(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"small_4x4x64", 4, 4, 64},
		{"medium_64x64x512", 64, 64, 512},
		{"large_1024x1024x1024", 1024, 1024, 1024},
		{"xlarge_4096x12288x8192", 4096, 12288, 8192},
	}

	for _, sz := range sizes {
		dataBytes := sz.k * sz.rowSize

		b.Run(sz.name, func(b *testing.B) {
			config := &Config{K: sz.k, N: sz.n, RowSize: sz.rowSize, WorkerCount: 1}

			// generate test data once
			data := make([][]byte, sz.k)
			for i := range sz.k {
				data[i] = make([]byte, sz.rowSize)
				for j := range sz.rowSize {
					data[i][j] = byte(rand.IntN(256))
				}
			}

			b.Run("old_Encode", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				for b.Loop() {
					_, _, _, err := Encode(data, config)
					if err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("new_Coder", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				coder, err := NewCoder(&Config{K: sz.k, N: sz.n, WorkerCount: 1})
				if err != nil {
					b.Fatal(err)
				}

				// pre-allocate rows
				rows := make([][]byte, sz.k+sz.n)
				for i := range sz.k {
					rows[i] = data[i]
				}
				for i := sz.k; i < sz.k+sz.n; i++ {
					rows[i] = make([]byte, sz.rowSize)
				}

				for b.Loop() {
					// clear parity to trigger fast encode path
					for i := sz.k; i < sz.k+sz.n; i++ {
						clear(rows[i])
					}
					_, _, _, err := coder.Encode(rows)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkVerifierCreateVerificationContext(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"small_4x4x64", 4, 4, 64},
		{"medium_64x64x512", 64, 64, 512},
		{"large_1024x1024x1024", 1024, 1024, 1024},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			config := &Config{K: sz.k, N: sz.n, RowSize: sz.rowSize, WorkerCount: 1}

			// generate test data and encode to get rlcOrig
			data := make([][]byte, sz.k)
			for i := range sz.k {
				data[i] = make([]byte, sz.rowSize)
				for j := range sz.rowSize {
					data[i][j] = byte(rand.IntN(256))
				}
			}

			_, _, rlcOrig, _ := Encode(data, config)

			b.Run("old_standalone", func(b *testing.B) {
				for b.Loop() {
					_, _, err := CreateVerificationContext(rlcOrig, config)
					if err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("new_Verifier", func(b *testing.B) {
				verifier, _ := NewVerifier(&Config{K: sz.k, N: sz.n, WorkerCount: 1})

				for b.Loop() {
					_, _, err := verifier.CreateVerificationContext(rlcOrig)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkRSReconstruct(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"small_4x4x64", 4, 4, 64},
		{"medium_64x64x512", 64, 64, 512},
		{"large_1024x1024x1024", 1024, 1024, 1024},
	}

	for _, sz := range sizes {
		dataBytes := sz.k * sz.rowSize

		b.Run(sz.name, func(b *testing.B) {
			// create encoder and encode test data
			enc, _ := reedsolomon.New(sz.k, sz.n, reedsolomon.WithLeopardGF16(true))

			shards := make([][]byte, sz.k+sz.n)
			for i := range sz.k {
				shards[i] = make([]byte, sz.rowSize)
				for j := range sz.rowSize {
					shards[i][j] = byte(rand.IntN(256))
				}
			}
			for i := sz.k; i < sz.k+sz.n; i++ {
				shards[i] = make([]byte, sz.rowSize)
			}
			enc.Encode(shards)

			// use half original, half parity indices
			indices := make([]int, sz.k)
			halfK := sz.k / 2
			for i := range halfK {
				indices[i] = i
			}
			for i := range sz.k - halfK {
				indices[halfK+i] = sz.k + i
			}

			// save source rows
			srcRows := make([][]byte, sz.k)
			for i, idx := range indices {
				srcRows[i] = make([]byte, sz.rowSize)
				copy(srcRows[i], shards[idx])
			}

			b.Run("new_encoder_each_time", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				testShards := make([][]byte, sz.k+sz.n)

				for b.Loop() {
					enc, _ := reedsolomon.New(sz.k, sz.n, reedsolomon.WithLeopardGF16(true))

					for i := range testShards {
						testShards[i] = nil
					}
					for i, idx := range indices {
						testShards[idx] = srcRows[i]
					}

					enc.Reconstruct(testShards)
				}
			})

			b.Run("reuse_encoder", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				testShards := make([][]byte, sz.k+sz.n)

				for b.Loop() {
					for i := range testShards {
						testShards[i] = nil
					}
					for i, idx := range indices {
						testShards[idx] = srcRows[i]
					}

					enc.Reconstruct(testShards)
				}
			})
		})
	}
}

func BenchmarkCoderReconstruct(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"small_4x4x64", 4, 4, 64},
		{"medium_64x64x512", 64, 64, 512},
		{"large_1024x1024x1024", 1024, 1024, 1024},
	}

	for _, sz := range sizes {
		dataBytes := sz.k * sz.rowSize

		b.Run(sz.name, func(b *testing.B) {
			config := &Config{K: sz.k, N: sz.n, RowSize: sz.rowSize, WorkerCount: 1}

			// generate and encode test data
			data := make([][]byte, sz.k)
			for i := range sz.k {
				data[i] = make([]byte, sz.rowSize)
				for j := range sz.rowSize {
					data[i][j] = byte(rand.IntN(256))
				}
			}

			extData, commitment, _, err := Encode(data, config)
			if err != nil {
				b.Fatal(err)
			}

			// use mixed indices (half original, half parity)
			indices := make([]int, sz.k)
			halfK := sz.k / 2
			for i := range halfK {
				indices[i] = i
			}
			for i := range sz.k - halfK {
				indices[halfK+i] = sz.k + i
			}

			// extract rows for reconstruction
			rows := make([][]byte, sz.k)
			for i, idx := range indices {
				rows[i] = extData.rows[idx]
			}

			b.Run("old_Reconstruct", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				for b.Loop() {
					_, err := Reconstruct(rows, indices, config)
					if err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("new_Reconstruction", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				coder, _ := NewCoder(&Config{K: sz.k, N: sz.n, WorkerCount: 1})

				// generate proofs once
				proofs := make([]*RowInclusionProof, sz.k)
				for i, idx := range indices {
					proofs[i], _ = extData.GenerateRowInclusionProof(idx)
				}

				for b.Loop() {
					recon := NewReconstruction(coder, commitment)
					for _, proof := range proofs {
						if err := recon.SetRow(proof); err != nil {
							b.Fatal(err)
						}
					}
					_, _, err := recon.Finish()
					if err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("new_Reconstruction_no_verify", func(b *testing.B) {
				b.SetBytes(int64(dataBytes))
				coder, _ := NewCoder(&Config{K: sz.k, N: sz.n, WorkerCount: 1})

				for b.Loop() {
					reconRows := make([][]byte, sz.k+sz.n)
					for i, idx := range indices {
						reconRows[idx] = rows[i]
					}
					_, _, _, err := coder.Encode(reconRows)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
