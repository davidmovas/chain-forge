package rpc_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	chain2 "github.com/davidmovas/chain-forge/chain"
	rpc2 "github.com/davidmovas/chain-forge/node/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	keyStoreDir   = ".keystorenode"
	blockStoreDir = ".blockstorenode"
	chainName     = "testblockchain"
	authPass      = "password"
	ownerPass     = "password"
	ownerBalance  = 1000
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

func grpcClientConn(
	t *testing.T, grpcRegisterSrv func(grpcSrv *grpc.Server),
) *grpc.ClientConn {
	// Set up the gRPC server
	lis := bufconn.Listen(1024 * 1024)
	grpcSrv := grpc.NewServer()
	grpcRegisterSrv(grpcSrv)
	go func() {
		err := grpcSrv.Serve(lis)
		if err != nil {
			fmt.Println(err)
		}
	}()
	// Set up the gRPC client
	resolver.SetDefaultScheme("passthrough")
	conn, err := grpc.NewClient(
		"bufnet",
		grpc.WithContextDialer(
			func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			},
		),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Set up the clean up of the gRPC client and server
	t.Cleanup(func() {
		lis.Close()
		grpcSrv.GracefulStop()
		conn.Close()
	})
	return conn
}

func TestAccountCreate(t *testing.T) {
	defer os.RemoveAll(keyStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Set up the gRPC server and client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		acc := rpc2.NewAccountSrv(keyStoreDir, nil)
		rpc2.RegisterAccountServer(grpcSrv, acc)
	})
	// Create the gRPC account client
	cln := rpc2.NewAccountClient(conn)
	req := &rpc2.AccountCreateReq{Password: ownerPass}
	// Call the AccountCrate method to create and persist a new account
	res, err := cln.AccountCreate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// Verify that the created account can be read from the local key store
	path := filepath.Join(keyStoreDir, res.Address)
	_, err = chain2.ReadAccount(path, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccountBalance(t *testing.T) {
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
	// Set up the gRPC server and client
	conn := grpcClientConn(t, func(grpcSrv *grpc.Server) {
		acc := rpc2.NewAccountSrv(keyStoreDir, state)
		rpc2.RegisterAccountServer(grpcSrv, acc)
	})
	// Create the gRPC account client
	cln := rpc2.NewAccountClient(conn)
	t.Run("existing balance", func(t *testing.T) {
		// Call the AccountBalance method to get the balance of an existing account
		req := &rpc2.AccountBalanceReq{Address: string(ownerAcc)}
		res, err := cln.AccountBalance(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		// Verify that balance is correct
		got, exp := res.Balance, ownerBal
		if got != exp {
			t.Errorf("invalid balance: expected %v, got %v", exp, got)
		}
	})
	t.Run("non-existing balance", func(t *testing.T) {
		// Call the AccountBalance method to get the balance of a non-existing
		// account
		req := &rpc2.AccountBalanceReq{Address: "non-existing"}
		_, err := cln.AccountBalance(ctx, req)
		// Verify that the correct error is returned
		if err == nil {
			t.Fatalf("non-existing account exists: expected error, got none")
		}
		got, exp := status.Code(err), codes.NotFound
		if got != exp {
			t.Errorf("wrong error: expected %v, got %v", got, exp)
		}
	})
}
