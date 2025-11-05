package chain_test

import (
	"testing"

	chain2 "github.com/davidmovas/chain-forge/chain"
)

func TestTxSignTxVerifyTx(t *testing.T) {
	// Create a new account
	acc, err := chain2.NewAccount()
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
