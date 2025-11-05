package rpc_test

import (
	context "context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	chain2 "github.com/davidmovas/chain-forge/chain"
	rpc2 "github.com/davidmovas/chain-forge/node/rpc"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func searchTxs(
	t *testing.T, ctx context.Context,
	conn grpc.ClientConnInterface, req *rpc2.TxSearchReq,
) []chain2.SearchTx {
	// Create the gRPC transaction client
	cln := rpc2.NewTxClient(conn)
	// Call the TxSearch method to get the gRPC server stream of transactions that
	// match the search request
	stream, err := cln.TxSearch(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	txs := make([]chain2.SearchTx, 0)
	// Start receiving found transactions from the gRPC server stream
	for {
		// Receive a transaction from the server stream
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		// Decode the received transaction
		jtx := res.Tx
		var tx chain2.SearchTx
		err = json.Unmarshal(jtx, &tx)
		if err != nil {
			t.Fatal(err)
		}
		// Append the decoded transaction to the list of found transactions
		txs = append(txs, tx)
	}
	return txs
}

func verifyTx(
	t *testing.T, ctx context.Context, cln rpc2.TxClient, tx chain2.SigTx,
	merkleRoot string,
) bool {
	// Call the TxProve method to derive the Merkle proof for the requested
	// transaction hash
	txh := tx.Hash().String()
	preq := &rpc2.TxProveReq{Hash: txh}
	pres, err := cln.TxProve(ctx, preq)
	if err != nil {
		t.Fatal(err)
	}
	// Call the TxVerify method to verify the derived Merkle proof for the
	// requested transaction hash and the provided Merkle root
	vreq := &rpc2.TxVerifyReq{
		Hash: txh, MerkleProof: pres.MerkleProof, MerkleRoot: merkleRoot,
	}
	vres, err := cln.TxVerify(ctx, vreq)
	if err != nil {
		t.Fatal(err)
	}
	return vres.Valid
}

func TestTxSign(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create and persist the genesis
	gen, err := createGenesis()
	if err != nil {
		t.Fatal(err)
	}
	// Create the state from the genesis
	state := chain2.NewState(gen)
	// Create and persist a new account
	acc, err := createAccount()
	if err != nil {
		t.Fatal(err)
	}
	// Set up the gRPC server and client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		tx := rpc2.NewTxSrv(keyStoreDir, blockStoreDir, state, nil)
		rpc2.RegisterTxServer(grpcSrv, tx)
	})
	// Create the gRPC transaction client
	cln := rpc2.NewTxClient(conn)
	// Call the TxSign method to sign the new transaction
	req := &rpc2.TxSignReq{
		From: string(acc.Address()), To: "to", Value: 12, Password: ownerPass,
	}
	res, err := cln.TxSign(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// Decode the signed transaction
	jtx := res.Tx
	var tx chain2.SigTx
	err = json.Unmarshal(jtx, &tx)
	if err != nil {
		t.Fatal(err)
	}
	// Verify that the signature of the signed transaction is valid
	valid, err := chain2.VerifyTx(tx)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Errorf("invalid transaction signature")
	}
}

func TestTxSend(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create and persist the genesis
	gen, err := createGenesis()
	if err != nil {
		t.Fatal(err)
	}
	// Create the state from the genesis
	state := chain2.NewState(gen)
	// Get the initial owner account and its balance from the genesis
	ownerAcc, ownerBal := genesisAccount(gen)
	// Re-create the initial owner account from the genesis
	path := filepath.Join(keyStoreDir, string(ownerAcc))
	acc, err := chain2.ReadAccount(path, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
	// Set up the gRPC server and client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		tx := rpc2.NewTxSrv(keyStoreDir, blockStoreDir, state.Pending, nil)
		rpc2.RegisterTxServer(grpcSrv, tx)
	})
	// Create the gRPC transaction client
	cln := rpc2.NewTxClient(conn)
	// Define several valid and invalid transactions
	cases := []struct {
		name  string
		value uint64
		err   error
	}{
		{"valid tx", 12, nil},
		{"insufficient funds", 1000, fmt.Errorf("insufficient funds")},
	}
	// Start sending transactions to the node
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create and sign a transaction
			tx := chain2.NewTx(
				acc.Address(), chain2.Address("to"), c.value,
				state.Pending.Nonce(acc.Address())+1,
			)
			stx, err := acc.SignTx(tx)
			if err != nil {
				t.Fatal(err)
			}
			// Call the TxSend method to send the signed transaction to the node
			jtx, err := json.Marshal(stx)
			if err != nil {
				t.Fatal(err)
			}
			req := &rpc2.TxSendReq{Tx: jtx}
			res, err := cln.TxSend(ctx, req)
			if c.err == nil && err != nil {
				t.Error(err)
			}
			// Verify that the valid transactions are accepted and the invalid
			// transactions are rejected
			if c.err != nil && err == nil {
				t.Errorf("expected TxSend error, got none")
			}
			if err != nil {
				got, exp := status.Code(err), codes.FailedPrecondition
				if got != exp {
					t.Errorf("wrong error: expected %v, got %v", exp, got)
				}
			}
			if err == nil {
				got, exp := res.Hash, stx.Hash().String()
				if got != exp {
					t.Errorf("invalid transaction hash")
				}
			}
		})
	}
	// Verify that the balance of the initial owner account on the pending state
	// is correct
	got, exist := state.Pending.Balance(acc.Address())
	exp := ownerBal - 12
	if !exist {
		t.Fatalf("balance does not exist")
	}
	if got != exp {
		t.Errorf("invalid balance: expected %v, got %v", exp, got)
	}
}

func TestTxReceive(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create and persist the genesis
	gen, err := createGenesis()
	if err != nil {
		t.Fatal(err)
	}
	// Create the state from the genesis
	state := chain2.NewState(gen)
	pending := state.Pending
	// Get the initial owner account and its balance from the genesis
	ownerAcc, ownerBal := genesisAccount(gen)
	// Re-create the initial owner account from the genesis
	path := filepath.Join(keyStoreDir, string(ownerAcc))
	acc, err := chain2.ReadAccount(path, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
	// Set up the gRPC server and gRPC client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		tx := rpc2.NewTxSrv(keyStoreDir, blockStoreDir, pending, nil)
		rpc2.RegisterTxServer(grpcSrv, tx)
	})
	// Create the gRPC transaction client
	cln := rpc2.NewTxClient(conn)
	// Call the TxReceive method to get the gRPC client stream to relay validated
	// transactions
	stream, err := cln.TxReceive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.CloseAndRecv()
	// Start relaying valid and invalid transactions to the gRPC client stream
	for _, value := range []uint64{12, 1000} {
		// Create and sign a transaction
		tx := chain2.NewTx(
			acc.Address(), chain2.Address("to"), value,
			pending.Nonce(acc.Address())+1,
		)
		stx, err := acc.SignTx(tx)
		if err != nil {
			t.Fatal(err)
		}
		// Encode the signed transaction
		jtx, err := json.Marshal(stx)
		if err != nil {
			t.Fatal(err)
		}
		// Call the gRPC TxReceive method to relay the encoded transaction
		req := &rpc2.TxReceiveReq{Tx: jtx}
		err = stream.Send(req)
		if err != nil {
			t.Fatal(err)
		}
		// Wait for the relayed transaction to be received and processed
		time.Sleep(50 * time.Millisecond)
	}
	// Verify that the balance of the initial owner account on the pending state
	// after receiving relayed transactions is correct
	got, exist := pending.Balance(acc.Address())
	if !exist {
		t.Errorf("balance does not exist %v", acc.Address())
	}
	exp := ownerBal - 12
	if got != exp {
		t.Errorf("invalid balance: expected %v, got %v", exp, got)
	}
}

func TestTxSearch(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create and persist the genesis
	gen, err := createGenesis()
	if err != nil {
		t.Fatal(err)
	}
	// Create the state from the genesis
	state := chain2.NewState(gen)
	// Create several confirmed blocks on the state and on the local block store
	err = createBlocks(gen, state)
	if err != nil {
		t.Fatal(err)
	}
	// Set up the gRPC server and client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		tx := rpc2.NewTxSrv(keyStoreDir, blockStoreDir, state.Pending, nil)
		rpc2.RegisterTxServer(grpcSrv, tx)
	})
	var hash chain2.Hash
	t.Run("search by sender account address", func(t *testing.T) {
		// Get the initial owner account from the genesis
		ownerAcc, _ := genesisAccount(gen)
		// Search transactions by the sender account address that equals to the
		// initial owner account address
		req := &rpc2.TxSearchReq{From: string(ownerAcc)}
		txs := searchTxs(t, ctx, conn, req)
		// Verify that all transactions are found
		got, exp := len(txs), 2
		if got != exp {
			t.Errorf("not all transactions are found: expected %v, got %v", exp, got)
		}
		// Verify that all found transactions satisfy the search criteria
		for _, tx := range txs {
			if (hash == chain2.Hash{}) {
				hash = tx.Hash()
			}
			if tx.From != ownerAcc {
				t.Errorf("invalid transaction: wrong sender address")
			}
		}
	})
	t.Run("search by transaction hash", func(t *testing.T) {
		// Search transactions by the transaction hash of an existing transaction
		req := &rpc2.TxSearchReq{Hash: hash.String()}
		txs := searchTxs(t, ctx, conn, req)
		// Verify that the transaction is found
		if len(txs) != 1 {
			t.Errorf("transaction by hash is not found")
		}
		// Verify that the found transaction matches the search criteria
		for _, tx := range txs {
			if tx.Hash() != hash {
				t.Errorf("invalid transaction hash")
			}
		}
	})
}

func TestTxProveVerify(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create and persist the genesis
	gen, err := createGenesis()
	if err != nil {
		t.Fatal(err)
	}
	// Create the state from the genesis
	state := chain2.NewState(gen)
	// Create several confirmed blocks on the state and on the local block store
	err = createBlocks(gen, state)
	if err != nil {
		t.Fatal(err)
	}
	// Set up the gRPC server and client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		tx := rpc2.NewTxSrv(keyStoreDir, blockStoreDir, state.Pending, nil)
		rpc2.RegisterTxServer(grpcSrv, tx)
	})
	// Create the gRPC transaction client
	cln := rpc2.NewTxClient(conn)
	// Get the initial owner account from the genesis
	acc, _ := genesisAccount(gen)
	// Search transactions by the sender account address that equals to the
	// initial owner account address
	req := &rpc2.TxSearchReq{From: string(acc)}
	txs := searchTxs(t, ctx, conn, req)
	// Verify that all transactions are found
	got, exp := len(txs), 2
	if got != exp {
		t.Errorf("not all transactions are found: expected %v, got %v", exp, got)
	}
	t.Run("correct Merkle proofs", func(t *testing.T) {
		// Verify that Merkle proofs for all found transactions are correct
		for _, tx := range txs {
			valid := verifyTx(t, ctx, cln, tx.SigTx, tx.MerkleRoot.String())
			if !valid {
				t.Errorf("invalid Merkle proof for transaction %v", tx.SigTx)
			}
		}
	})
	t.Run("incorrect Merkle proofs", func(t *testing.T) {
		// Verify that Merkle proofs for the invalid Merkle root are incorrect
		for _, tx := range txs {
			merkleRoot := chain2.NewHash("invalid Merkle root")
			valid := verifyTx(t, ctx, cln, tx.SigTx, merkleRoot.String())
			if valid {
				t.Errorf("valid Merkle proof for invalid Merkle root %v", tx.SigTx)
			}
		}
	})
}
