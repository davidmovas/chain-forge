package chain_test

import (
	"os"
	"path/filepath"
	"testing"

	chain2 "github.com/davidmovas/chain-forge/chain"
)

const (
	keyStoreDir   = ".keystorenode"
	blockStoreDir = ".keystorenode"
	chainName     = "testblockchain"
	authPass      = "password"
	ownerPass     = "password"
	ownerBalance  = 1000
)

func TestAccountWriteReadSignTxVerifyTx(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	// Create a new account
	acc, err := chain2.NewAccount()
	if err != nil {
		t.Fatal(err)
	}
	// Persist the new account
	err = acc.Write(keyStoreDir, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the persisted account
	path := filepath.Join(keyStoreDir, string(acc.Address()))
	acc, err = chain2.ReadAccount(path, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
	// Create and sign a transaction
	tx := chain2.NewTx(acc.Address(), chain2.Address("to"), 12, 1)
	stx, err := acc.SignTx(tx)
	if err != nil {
		t.Fatal(err)
	}
	// Verify that the signature of the signed transaction is valid
	valid, err := chain2.VerifyTx(stx)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Errorf("invalid transaction signature")
	}
}
