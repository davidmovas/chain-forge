package chain_test

import (
	"fmt"
	"slices"
	"testing"

	chain2 "github.com/davidmovas/chain-forge/chain"
)

func strRange(end int) []string {
	slc := make([]string, end)
	for i := range end {
		slc[i] = fmt.Sprintf("%v", i+1)
	}
	return slc
}

func typeHash(s string) chain2.Hash {
	return chain2.NewHash(s)
}

func formatMerkleTree(merkleTree []chain2.Hash) string {
	mt := make([]string, len(merkleTree))
	for i := range merkleTree {
		mt[i] = fmt.Sprintf("%.4s", merkleTree[i])
	}
	return fmt.Sprintf("%v", mt)
}

func formatMerkleProof(merkleProof []chain2.Proof[chain2.Hash]) string {
	mp := make([]string, len(merkleProof))
	for i, proof := range merkleProof {
		var pos string
		if proof.Pos == chain2.Left {
			pos = "L"
		} else {
			pos = "R"
		}
		mp[i] = fmt.Sprintf("%.4s-%v", proof.Hash, pos)
	}
	return fmt.Sprintf("%v", mp)
}

func TestMerkleHashProveVerify(t *testing.T) {
	for i := range 9 {
		// Generate lists of transactions starting from ["1"] to ["1".."9"]
		// inclusive
		txs := strRange(i + 1)
		// Construct the Merkle tree for the generated list of transactions
		// using the provided transaction hash function and the pair hash function
		merkleTree, err := chain2.MerkleHash(txs, typeHash, chain2.TxPairHash)
		if err != nil {
			t.Fatal(err)
		}
		// Print the array representation of the constructed Merkle tree
		fmt.Printf("Tree (%v) %v\n", len(txs), formatMerkleTree(merkleTree))
		merkleRoot := merkleTree[0]
		// Start iterating over the transactions from the generated transaction list
		for _, tx := range txs {
			txh := typeHash(tx)
			// Derive the Merkle proof for the transaction hash from the constructed
			// Merkle tree
			merkleProof, err := chain2.MerkleProve(txh, merkleTree)
			if err != nil {
				t.Fatal(err)
			}
			// Print the derived Merkle proof
			fmt.Printf("Proof %v %.4s %v ", tx, txh, formatMerkleProof(merkleProof))
			// Verify the derived Merkle proof for the transaction hash and the
			// constructed Merkle root
			valid := chain2.MerkleVerify(txh, merkleProof, merkleRoot, chain2.TxPairHash)
			// Verify that the derived Merkle proof is correct
			if valid {
				fmt.Println("valid")
			} else {
				fmt.Println("INVALID")
			}
			if !valid {
				t.Errorf(
					"invalid Merkle proof: %v %.4s %v",
					tx, txh, formatMerkleProof(merkleProof),
				)
			}
		}
	}
}

func typeHashStr(s string) string {
	return s
}

func pairHashStr(l, r string) string {
	if r == "" {
		return l
	}
	return l + r
}

func formatMerkleTreeStr(merkleTree []string) string {
	mt := slices.Clone(merkleTree)
	for i := range mt {
		if mt[i] == "" {
			mt[i] = "_"
		}
	}
	return fmt.Sprintf("%v", mt)
}

func formatMerkleProofStr(merkleProof []chain2.Proof[string]) string {
	mp := make([]string, len(merkleProof))
	for i, proof := range merkleProof {
		var pos string
		if proof.Pos == chain2.Left {
			pos = "L"
		} else {
			pos = "R"
		}
		mp[i] = fmt.Sprintf("%v-%v", proof.Hash, pos)
	}
	return fmt.Sprintf("%v", mp)
}

func TestMerkleHashProveVerifyStr(t *testing.T) {
	for i := range 9 {
		txs := strRange(i + 1)
		merkleTree, err := chain2.MerkleHash(txs, typeHashStr, pairHashStr)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("Tree (%v) %v\n", len(txs), formatMerkleTreeStr(merkleTree))
		merkleRoot := merkleTree[0]
		for _, tx := range txs {
			txh := typeHashStr(tx)
			merkleProof, err := chain2.MerkleProve(txh, merkleTree)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Printf("Proof %v %v ", txh, formatMerkleProofStr(merkleProof))
			valid := chain2.MerkleVerify(txh, merkleProof, merkleRoot, pairHashStr)
			if valid {
				fmt.Println("valid")
			} else {
				fmt.Println("INVALID")
			}
			if !valid {
				t.Errorf(
					"invalid Merkle proof: %v %v", txh, formatMerkleProofStr(merkleProof),
				)
			}
		}
	}
}
