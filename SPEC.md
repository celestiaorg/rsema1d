# RSEMA1D Codec Specification v1.0

## 1. Overview

RSEMA1D (Reed-Solomon Evans-Mohnblatt-Angeris 1D) is a data availability codec that provides efficient commitment, proof generation, and verification for vertically-extended data matrices. The codec uses random linear combinations (RLCs) to ensure soundness of the encoding.

### Key Properties
- **Vertical-only extension**: Reed-Solomon encoding applied only along columns using Leopard codec
- **RLC-based verification**: Uses random linear combinations for soundness
- **Efficient verification**: O(K) operations for verifying extended rows, O(log K) for original rows
- **128-bit security**: Using GF(2^128) for random linear combinations against RLC forgery

## 2. Mathematical Foundations

### 2.1 Field Definitions

**GF(2^16)**: The base field for Reed-Solomon encoding
- Irreducible polynomial: x^16 + x^12 + x^3 + x + 1 (0x1002D)
- Used by Leopard RS codec for efficient encoding/decoding

**Leopard Codec**: 
- Uses systematic Reed-Solomon encoding
- Each row is treated as a single Leopard shard
- Shards must be at least 64 bytes and must be a multiple of 64 bytes
- **Internal format**: Leopard processes data in 64-byte chunks with interleaved GF(2^16) format:
  - Each 64-byte chunk: bytes 0-31 contain low bytes, bytes 32-63 contain high bytes of 32 GF(2^16) symbols
  - Symbol_i = (byte[32+i] << 8) | byte[i] for i ∈ [0,32)
  - Rows > 64 bytes are treated as concatenated 64-byte chunks

**GF(2^128)**: The field for random linear combinations
- Represented as 8-dimensional vector space over GF(2^16)
- Type representation: array of 8 uint16 values (little-endian)
- Operations:
  - Addition: component-wise XOR
  - Scalar multiplication (GF16 × GF128): multiply each component by scalar

### 2.2 Data Layout

Data is arranged as a tall matrix:
```
Original Data:     Extended Data:
┌───────────┐      ┌───────────┐
│  K rows   │      │  K rows   │ (original)
│           │      │           │
└───────────┘      ├───────────┤
                   │  N rows   │ (parity)
                   │           │
                   └───────────┘
```

Each row contains `rowSize` bytes, where:
- `rowSize` must be a multiple of 64 (Leopard constraint)
- When processing RLCs, each row is interpreted as `rowSize/2` GF(2^16) symbols (since each GF(2^16) symbol is 2 bytes)

## 3. Codec Specification

### 3.1 Parameters

```
K:       Number of original rows (power of 2, ≤ 2^16)
N:       Number of parity rows (power of 2, ≤ 2^16, typically N = K)
rowSize: Size of each row in bytes (multiple of 64)
```

### 3.2 Data Extension

**Input**: K rows of `rowSize` bytes each

**Process**:
1. Treat each row as a Leopard shard (rowSize bytes)
2. Apply Leopard RS encoding:
   - Input: K shards (original rows)
   - Output: N parity shards (parity rows)
   - This is done in a single Leopard encoding call
3. Result: K+N total rows

**Output**: K+N rows of `rowSize` bytes each

### 3.3 Commitment Generation

**Input**: Extended data (K+N rows)

**Process**:

1. **Compute Row Root**
   ```
   for i in 0..(K+N):
       rowHashes[i] = SHA256(row[i])
   rowTree = MerkleTree(rowHashes)
   rowRoot = rowTree.root()
   ```

2. **Derive RLC Coefficients**
   ```
   seed = SHA256(rowRoot)
   numChunks = rowSize / 64
   for c in 0..numChunks:
       for j in 0..32:  // 32 symbols per 64-byte chunk
           coeffs[c][j] = ExpandToGF128(SHA256(seed || c || j))
   ```
   Where ExpandToGF128 generates 8 GF(2^16) values from 32-byte hash

3. **Compute RLC Results (Original Rows Only)**
   ```
   for i in 0..K:
       y[i] = 0  // Initialize as zero in GF(2^128)
       for c in 0..numChunks:
           chunk = row[i][c*64..(c+1)*64]
           symbols = ExtractSymbols(chunk)  // See Appendix A.1
           for j in 0..32:
               y[i] += symbols[j] * coeffs[c][j]  // GF16 × GF128
   ```

4. **Extend RLC Results**
   ```
   // Treat y[0..K] as K symbols in GF(2^128)
   // Apply RS encoding to generate N parity symbols
   y_extended = LeopardExtend(y[0..K], K, N)
   ```

5. **Compute RLC Root**
   ```
   for i in 0..(K+N):
       rlcLeaves[i] = Serialize(y_extended[i])  // 16 bytes each
   rlcTree = MerkleTree(rlcLeaves)
   rlcRoot = rlcTree.root()
   ```

6. **Final Commitment**
   ```
   commitment = SHA256(rowRoot || rlcRoot)
   ```

**Output**: 
- `commitment`: 32-byte commitment
- `rowRoot`: 32-byte Merkle root of row hashes
- `rlcRoot`: 32-byte Merkle root of RLC results

### 3.4 Proof Generation

**Input**: Row index i, extended data, commitment

**Process**:

1. **Include Row Data**
   ```
   proof.row = row[i]
   proof.index = i
   ```

2. **Generate Row Merkle Proof**
   ```
   proof.rowProof = rowTree.generateProof(i)
   ```

3. **For Extended Rows (i ≥ K):**
   - **Include Original RLC Results**
     ```
     for j in 0..K:
         proof.yOrig[j] = Serialize(y[j])  // 16 bytes each
     ```
   - **Generate Left-Subtree Proof**
     ```
     leftSubtreeRoot = MerkleTree(rlcLeaves[0..K]).root()
     proof.yLeftProof = rlcTree.generateProofFromSubtree(leftSubtreeRoot)
     ```

4. **For Original Rows (i < K):**
   - **Generate RLC Merkle Proof**
     ```
     proof.rlcProof = rlcTree.generateProof(i)
     ```

**Output**: Proof containing:
- `index`: Row index
- `row`: Row data (rowSize bytes)
- `rowProof`: Merkle proof for row (log2(K+N) × 32 bytes)
- For extended rows (i ≥ K):
  - `yOrig`: Original RLC results (K × 16 bytes)
  - `yLeftProof`: Proof from left-subtree to rlcRoot (log2(K+N) × 32 bytes)
- For original rows (i < K):
  - `rlcProof`: Merkle proof for RLC result (log2(K+N) × 32 bytes)

### 3.5 Proof Verification

**Input**: Proof, commitment, parameters

**Process**:

1. **Verify Row Inclusion**
   ```
   rowHash = SHA256(proof.row)
   assert MerkleVerify(rowHash, proof.index, proof.rowProof, commitment.rowRoot)
   ```

2. **Recompute RLC**
   ```
   coeffs = DeriveCoefficients(commitment.rowRoot, params)  // Same as in 3.3.2
   y_i = 0
   for c in 0..numChunks:
       chunk = proof.row[c*64..(c+1)*64]
       symbols = ExtractSymbols(chunk)  // See Appendix A.1
       for j in 0..32:
           y_i += symbols[j] * coeffs[c][j]
   ```

3. **For Original Rows (proof.index < K):**
   ```
   // Verify RLC is in commitment
   assert MerkleVerify(y_i, proof.index, proof.rlcProof, commitment.rlcRoot)
   ```

4. **For Extended Rows (proof.index ≥ K):**
   ```
   // Verify left-subtree
   leftSubtreeRoot = MerkleTree(proof.yOrig).root()
   assert MerkleVerify(leftSubtreeRoot, proof.yLeftProof, commitment.rlcRoot)
   
   // Verify extension consistency
   y_extended = LeopardExtend(proof.yOrig, K, N)
   assert y_i == y_extended[proof.index]
   
   // Verify complete RLC tree
   rlcLeaves = Serialize(y_extended)
   recomputedRlcRoot = MerkleTree(rlcLeaves).root()
   assert recomputedRlcRoot == commitment.rlcRoot
   ```

5. **Verify Commitment Integrity**
   ```
   // Final step: verify the commitment hash matches the roots
   assert SHA256(commitment.rowRoot || commitment.rlcRoot) == commitment.hash
   ```

**Output**: Accept/Reject

## 4. Security Considerations

### 4.1 Soundness
The codec provides:
- **RLC Forgery Resistance**: 128-bit security against forging invalid RLC values (GF(2^128) field size)
- **Commitment Binding**: SHA-256 collision resistance ensures commitment uniqueness
- **Encoding Soundness**: The overall encoding soundness depends on the number of random samples verified by the application layer (out of scope for this library)

### 4.2 Proximity Gap
The Reed-Solomon code provides distance properties:
- Minimum distance: N+1 symbols
- Any K rows can reconstruct the original data
- Detection of up to N errors

### 4.3 Commitment Binding
The commitment is binding due to:
- Collision-resistant hash functions
- Merkle tree structure
- Deterministic coefficient derivation

## 5. Implementation Requirements

### 5.1 Cryptographic Primitives
- SHA-256 hash function
- Binary Merkle tree implementation
- Leopard Reed-Solomon codec (or compatible)

### 5.2 Arithmetic Operations
- GF(2^16) multiplication and addition
- GF(2^128) as vector space operations
- Efficient polynomial evaluation

### 5.3 Memory Requirements
- Prover: O(K × rowSize) for data storage
- Verifier for original rows: O(rowSize) for row data + O(log(K+N)) for proofs
- Verifier for extended rows: O(K × 16) for RLC results + O(rowSize) for row data
- Proof size for original rows: O(log(K+N) × 32) bytes
- Proof size for extended rows: O(K × 16 + log(K+N) × 32) bytes

## 6. Test Vectors

### 6.1 Small Example (K=4, N=4, rowSize=64)

**TODO**: Fill in expected outputs after implementation

Input data (4 rows × 64 bytes):
```
Row 0: 0x0000000000000000...0000000000000001
Row 1: 0x0000000000000000...0000000000000002
Row 2: 0x0000000000000000...0000000000000003
Row 3: 0x0000000000000000...0000000000000004
```

Expected outputs:
```
rowRoot:    [32 bytes - dependent on exact Merkle tree implementation]
rlcRoot:    [32 bytes - dependent on coefficient derivation]
commitment: [32 bytes - SHA256 of concatenated roots]
```

### 6.2 Verification Test Cases

1. **Valid Original Row**: Proof for row 0 should verify
2. **Valid Parity Row**: Proof for row 4 should verify
3. **Invalid Row Data**: Modified row should fail verification
4. **Invalid RLC Result**: Modified yOrig should fail verification
5. **Wrong Commitment**: Different commitment should fail

## 7. References

- Leopard Reed-Solomon: https://github.com/catid/leopard
- EMA Paper: https://eprint.iacr.org/2025/034
- GF(2^16) arithmetic: Leopard field specification
- SHA-256: FIPS 180-4
- Merkle Trees: RFC 6962 (adapted for binary trees)

## Appendix A: Leopard-Specific Operations

### A.1 Symbol Extraction from Leopard Share
Leopard uses an interleaved format for GF(2^16) symbols within 64-byte chunks:

```
ExtractSymbols(chunk[64]): // Returns 32 GF(2^16) symbols
    for i in 0..32:
        symbol[i] = (chunk[32+i] << 8) | chunk[i]
    return symbol[0..32]
```

Each 64-byte chunk independently contains 32 symbols. For rows larger than 64 bytes, each chunk is processed separately.

### A.2 Visual Representation of Leopard Format

**64-byte chunk format:**
```
┌─────────────────── 64-byte Leopard chunk ─────────────────────┐
│                                                               │
│  Bytes 0-31: Low bytes of 32 GF(2^16) symbols                 │
│  [L₀][L₁][L₂]...[L₃₁]                                         │
│                                                               │
│  Bytes 32-63: High bytes of 32 GF(2^16) symbols               │
│  [H₀][H₁][H₂]...[H₃₁]                                         │
│                                                               │
└───────────────────────────────────────────────────────────────┘

Each GF(2^16) symbol i = (Hᵢ << 8) | Lᵢ
```

**256-byte row (4 chunks):**
```
┌─────────────────── 256-byte Leopard row ──────────────────────┐
│                                                               │
│  Chunk 0 (bytes 0-63):                                        │
│    ├─ Bytes 0-31:   Low bytes of symbols 0-31                 │
│    └─ Bytes 32-63:  High bytes of symbols 0-31                │
│                                                               │
│  Chunk 1 (bytes 64-127):                                      │
│    ├─ Bytes 64-95:  Low bytes of symbols 32-63                │
│    └─ Bytes 96-127: High bytes of symbols 32-63               │
│                                                               │
│  Chunk 2 (bytes 128-191):                                     │
│    ├─ Bytes 128-159: Low bytes of symbols 64-95               │
│    └─ Bytes 160-191: High bytes of symbols 64-95              │
│                                                               │
│  Chunk 3 (bytes 192-255):                                     │
│    ├─ Bytes 192-223: Low bytes of symbols 96-127              │
│    └─ Bytes 224-255: High bytes of symbols 96-127             │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

## Appendix B: Serialization Formats

### B.1 GF128 Serialization
16 bytes, little-endian encoding of 8 uint16 limbs:
```
bytes[0:2]   = limb[0] (little-endian uint16)
bytes[2:4]   = limb[1] (little-endian uint16)
...
bytes[14:16] = limb[7] (little-endian uint16)
```

### B.2 Merkle Tree Construction
- Binary tree with power-of-2 leaves
- Internal nodes: SHA256(left || right)
- Leaf nodes: Direct hash values (no double-hashing)

### B.3 Proof Serialization
Recommended format (implementers may choose alternatives):
```
[4 bytes]    index (uint32, little-endian)
[4 bytes]    rowSize (uint32, little-endian)
[rowSize]    row data
[4 bytes]    rowProofLen (uint32, little-endian)
[variable]   rowProof (concatenated 32-byte hashes)
[K × 16]     yOrig (serialized GF128 values)
[4 bytes]    yLeftProofLen (uint32, little-endian)
[variable]   yLeftProof (concatenated 32-byte hashes)
```