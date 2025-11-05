package chain_test

import (
	"os"
	"testing"

	chain2 "github.com/davidmovas/chain-forge/chain"
)

func createAccount() (chain2.Account, error) {
	acc, err := chain2.NewAccount()
	if err != nil {
		return chain2.Account{}, err
	}
	err = acc.Write(keyStoreDir, []byte(ownerPass))
	if err != nil {
		return chain2.Account{}, err
	}
	return acc, nil
}

func createGenesis() (chain2.SigGenesis, error) {
	auth, err := createAccount()
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	acc, err := createAccount()
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	gen := chain2.NewGenesis(chainName, auth.Address(), acc.Address(), ownerBalance)
	sgen, err := auth.SignGen(gen)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	err = sgen.Write(blockStoreDir)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	return sgen, nil
}

func genesisAccount(gen chain2.SigGenesis) (chain2.Address, uint64) {
	for acc, bal := range gen.Balances {
		return acc, bal
	}
	return "", 0
}

func TestGenesisWriteReadSignGenVerifyGen(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	// Create and persist the authority account to sign the genesis and proposed
	// blocks
	auth, err := createAccount()
	if err != nil {
		t.Fatal(err)
	}
	// Create and persist the initial owner account to hold the initial balance of
	// the blockchain
	acc, err := createAccount()
	if err != nil {
		t.Fatal(err)
	}
	// Create and persist the genesis
	gen := chain2.NewGenesis(chainName, auth.Address(), acc.Address(), ownerBalance)
	sgen, err := auth.SignGen(gen)
	if err != nil {
		t.Fatal(err)
	}
	err = sgen.Write(blockStoreDir)
	if err != nil {
		t.Fatal(err)
	}
	// Re-create the persisted genesis
	sgen, err = chain2.ReadGenesis(blockStoreDir)
	if err != nil {
		t.Fatal(err)
	}
	// Verify that the signature of the persisted genesis is valid
	valid, err := chain2.VerifyGen(sgen)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Errorf("invalid genesis signature")
	}
}
