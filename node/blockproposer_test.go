package node_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	chain2 "github.com/davidmovas/chain-forge/chain"
	node2 "github.com/davidmovas/chain-forge/node"
	rpc2 "github.com/davidmovas/chain-forge/node/rpc"
	"google.golang.org/grpc"
)

func createBlockRelay(
	ctx context.Context, wg *sync.WaitGroup, peerReader node2.PeerReader,
) *node2.MsgRelay[chain2.SigBlock, node2.GRPCMsgRelay[chain2.SigBlock]] {
	blkRelay := node2.NewMsgRelay(ctx, wg, 1, node2.GRPCBlockRelay, true, peerReader)
	wg.Add(1)
	go blkRelay.RelayMsgs(100 * time.Millisecond)
	return blkRelay
}

func createBlockProposer(
	ctx context.Context, wg *sync.WaitGroup, blkRelayer node2.BlockRelayer,
	auth chain2.Account, state *chain2.State,
) *node2.BlockProposer {
	blockProp := node2.NewBlockProposer(ctx, wg, blkRelayer)
	blockProp.SetAuthority(auth)
	blockProp.SetState(state)
	wg.Add(1)
	go blockProp.ProposeBlocks(400 * time.Millisecond)
	return blockProp
}

func TestBlockProposer(t *testing.T) {
	defer os.RemoveAll(bootKeyStoreDir)
	defer os.RemoveAll(bootBlockStoreDir)
	defer os.RemoveAll(keyStoreDir)
	defer os.RemoveAll(blockStoreDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := new(sync.WaitGroup)
	// Create the peer discovery without starting for the bootstrap node
	bootPeerDisc := createPeerDiscovery(ctx, wg, true, false)
	// Initialize the state on the bootstrap node by creating the genesis
	bootState, err := createStateSync(ctx, bootPeerDisc, true)
	if err != nil {
		t.Fatal(err)
	}
	// Create and start the block relay for the bootstrap node
	bootBlkRelay := createBlockRelay(ctx, wg, bootPeerDisc)
	// Re-create the authority account from the genesis to sign blocks
	path := filepath.Join(bootKeyStoreDir, string(bootState.Authority()))
	auth, err := chain2.ReadAccount(path, []byte(authPass))
	if err != nil {
		t.Fatal(err)
	}
	// Create and start the block proposer on the bootstrap node
	_ = createBlockProposer(ctx, wg, bootBlkRelay, auth, bootState)
	// Start the gRPC server on the bootstrap node
	grpcStartSvr(t, bootAddr, func(grpcSrv *grpc.Server) {
		node := rpc2.NewNodeSrv(bootPeerDisc, nil)
		rpc2.RegisterNodeServer(grpcSrv, node)
		tx := rpc2.NewTxSrv(
			bootKeyStoreDir, bootBlockStoreDir, bootState.Pending, nil,
		)
		rpc2.RegisterTxServer(grpcSrv, tx)
		blk := rpc2.NewBlockSrv(bootBlockStoreDir, nil, bootState, bootBlkRelay)
		rpc2.RegisterBlockServer(grpcSrv, blk)
	})
	// Create and start the peer discovery for the new node
	nodePeerDisc := createPeerDiscovery(ctx, wg, false, true)
	// Wait for the peer discovery to discover peers
	time.Sleep(150 * time.Millisecond)
	// Synchronize the state on the new node by fetching the genesis and confirmed
	// blocks from the bootstrap node
	nodeState, err := createStateSync(ctx, nodePeerDisc, false)
	if err != nil {
		t.Fatal(err)
	}
	// Start the gRPC server on the new node
	grpcStartSvr(t, nodeAddr, func(grpcSrv *grpc.Server) {
		tx := rpc2.NewTxSrv(keyStoreDir, blockStoreDir, nodeState.Pending, nil)
		rpc2.RegisterTxServer(grpcSrv, tx)
		blk := rpc2.NewBlockSrv(blockStoreDir, nil, nodeState, nil)
		rpc2.RegisterBlockServer(grpcSrv, blk)
	})
	// Wait for the gRPC server of the new node to start
	time.Sleep(100 * time.Millisecond)
	// Get the initial owner account and its balance from the genesis
	gen, err := chain2.ReadGenesis(bootBlockStoreDir)
	if err != nil {
		t.Fatal(err)
	}
	ownerAcc, ownerBal := genesisAccount(gen)
	// Re-create the initial owner account from the genesis
	path = filepath.Join(bootKeyStoreDir, string(ownerAcc))
	acc, err := chain2.ReadAccount(path, []byte(ownerPass))
	if err != nil {
		t.Fatal(err)
	}
	// Sign and send several signed transactions to the bootstrap node
	sendTxs(t, ctx, acc, []uint64{12, 34}, bootState.Pending, bootAddr)
	// Wait for the block proposal to propose a block and the block relay to
	// propagate the proposed block
	time.Sleep(500 * time.Millisecond)
	// Verify that the initial account balance on the confirmed state of the new
	// node and the bootstrap node are equal
	expBalance := ownerBal - 12 - 34
	nodeBalance, exist := nodeState.Balance(acc.Address())
	if !exist {
		t.Fatalf("balance does not exist on the new node")
	}
	if nodeBalance != expBalance {
		t.Errorf(
			"invalid node balance: expected %v, got %v", expBalance, nodeBalance,
		)
	}
	bootBalance, exist := bootState.Balance(acc.Address())
	if !exist {
		t.Fatalf("balance does not exist on the bootstrap node")
	}
	if bootBalance != expBalance {
		t.Errorf(
			"invalid bootstrap balance: expected %v, got %v", expBalance, bootBalance,
		)
	}
}
