package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	chain2 "github.com/davidmovas/chain-forge/chain"
	rpc2 "github.com/davidmovas/chain-forge/node/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type StateSync struct {
	cfg        NodeCfg
	ctx        context.Context
	state      *chain2.State
	peerReader PeerReader
}

func NewStateSync(
	ctx context.Context, cfg NodeCfg, peerReader PeerReader,
) *StateSync {
	return &StateSync{ctx: ctx, cfg: cfg, peerReader: peerReader}
}

func (s *StateSync) createGenesis() (chain2.SigGenesis, error) {
	authPass := []byte(s.cfg.AuthPass)
	if len(authPass) < 5 {
		return chain2.SigGenesis{}, fmt.Errorf("authpass length is less than 5")
	}
	auth, err := chain2.NewAccount()
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	err = auth.Write(s.cfg.KeyStoreDir, authPass)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	ownerPass := []byte(s.cfg.OwnerPass)
	if len(ownerPass) < 5 {
		return chain2.SigGenesis{}, fmt.Errorf("ownerpass length is less than 5")
	}
	if s.cfg.Balance == 0 {
		return chain2.SigGenesis{}, fmt.Errorf("balance must be positive")
	}
	acc, err := chain2.NewAccount()
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	err = acc.Write(s.cfg.KeyStoreDir, ownerPass)
	s.cfg.OwnerPass = "erase"
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	gen := chain2.NewGenesis(
		s.cfg.Chain, auth.Address(), acc.Address(), s.cfg.Balance,
	)
	sgen, err := auth.SignGen(gen)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	err = sgen.Write(s.cfg.BlockStoreDir)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	return sgen, nil
}

func (s *StateSync) grpcGenesisSync() ([]byte, error) {
	conn, err := grpc.NewClient(
		s.cfg.SeedAddr, grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	cln := rpc2.NewBlockClient(conn)
	req := &rpc2.GenesisSyncReq{}
	res, err := cln.GenesisSync(s.ctx, req)
	if err != nil {
		return nil, err
	}
	return res.Genesis, nil
}

func (s *StateSync) syncGenesis() (chain2.SigGenesis, error) {
	jgen, err := s.grpcGenesisSync()
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	var gen chain2.SigGenesis
	err = json.Unmarshal(jgen, &gen)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	valid, err := chain2.VerifyGen(gen)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	if !valid {
		return chain2.SigGenesis{}, fmt.Errorf("invalid genesis signature")
	}
	err = gen.Write(s.cfg.BlockStoreDir)
	if err != nil {
		return chain2.SigGenesis{}, err
	}
	return gen, nil
}

func (s *StateSync) readBlocks() error {
	blocks, closeBlocks, err := chain2.ReadBlocks(s.cfg.BlockStoreDir)
	if err != nil {
		return err
	}
	defer closeBlocks()
	for err, blk := range blocks {
		if err != nil {
			return err
		}
		clone := s.state.Clone()
		err = clone.ApplyBlock(blk)
		if err != nil {
			return err
		}
		s.state.Apply(clone)
	}
	return nil
}

func (s *StateSync) grpcBlockSync(peer string) (
	func(yield func(err error, jblk []byte) bool), func(), error,
) {
	conn, err := grpc.NewClient(
		peer, grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}
	close := func() {
		conn.Close()
	}
	cln := rpc2.NewBlockClient(conn)
	req := &rpc2.BlockSyncReq{Number: s.state.LastBlock().Number + 1}
	stream, err := cln.BlockSync(s.ctx, req)
	if err != nil {
		return nil, nil, err
	}
	more := true
	blocks := func(yield func(err error, jblk []byte) bool) {
		for more {
			res, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				yield(err, nil)
				return
			}
			more = yield(nil, res.Block)
		}
	}
	return blocks, close, nil
}

func (s *StateSync) syncBlocks() error {
	for _, peer := range s.peerReader.Peers() {
		blocks, closeBlocks, err := s.grpcBlockSync(peer)
		if err != nil {
			return err
		}
		defer closeBlocks()
		for err, jblk := range blocks {
			if err != nil {
				return err
			}
			var blk chain2.SigBlock
			err = json.Unmarshal(jblk, &blk)
			if err != nil {
				return err
			}
			clone := s.state.Clone()
			err = clone.ApplyBlock(blk)
			if err != nil {
				return err
			}
			s.state.Apply(clone)
			err = blk.Write(s.cfg.BlockStoreDir)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *StateSync) SyncState() (*chain2.State, error) {
	gen, err := chain2.ReadGenesis(s.cfg.BlockStoreDir)
	if err != nil {
		if s.cfg.Bootstrap {
			gen, err = s.createGenesis()
			if err != nil {
				return nil, err
			}
		} else {
			gen, err = s.syncGenesis()
			if err != nil {
				return nil, err
			}
		}
	}
	valid, err := chain2.VerifyGen(gen)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("invalid genesis signature")
	}
	s.state = chain2.NewState(gen)
	err = chain2.InitBlockStore(s.cfg.BlockStoreDir)
	if err != nil {
		return nil, err
	}
	err = s.readBlocks()
	if err != nil {
		return nil, err
	}
	err = s.syncBlocks()
	if err != nil {
		return nil, err
	}
	fmt.Printf("=== Sync state\n%v", s.state)
	return s.state, nil
}
