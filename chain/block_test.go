package chain_test

import (
	"os"
	"path/filepath"
	"testing"

	chain2 "github.com/davidmovas/chain-forge/chain"
)

func TestBlockSignBlockWriteReadVerifyBlock(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	// Create and persist the genesis
	gen, err := createGenesis()
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the authority account from the genesis
	path := filepath.Join(keyStoreDir, string(gen.Authority))
	auth, err := chain2.ReadAccount(path, []byte(authPass))
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the initial owner account from the genesis
	ownerAcc, _ := genesisAccount(gen)
	path = filepath.Join(keyStoreDir, string(ownerAcc))
	acc, err := chain2.ReadAccount(path, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
	// Create and sign a transaction with the initial owner account
	tx := chain2.NewTx(chain2.Address("from"), chain2.Address("to"), 12, 1)
	stx, err := acc.SignTx(tx)
	if err != nil {
		t.Fatal(err)
	}
	// Create and sign a block with the authority account
	txs := make([]chain2.SigTx, 0, 1)
	txs = append(txs, stx)
	blk, err := chain2.NewBlock(1, gen.Hash(), txs)
	if err != nil {
		t.Fatal(err)
	}
	sblk, err := auth.SignBlock(blk)
	if err != nil {
		t.Fatal(err)
	}
	// Persist the signed block
	err = sblk.Write(blockStoreDir)
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the signed block
	blocks, closeBlocs, err := chain2.ReadBlocks(blockStoreDir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBlocs()
	for err, blk := range blocks {
		if err != nil {
			t.Fatal(err)
		}
		// Verify that the signature of the signed block is valid
		valid, err := chain2.VerifyBlock(blk, auth.Address())
		if err != nil {
			t.Fatal(err)
		}
		if !valid {
			t.Errorf("invalid block signature")
		}
		break
	}
}
