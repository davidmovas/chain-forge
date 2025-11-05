package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	chain2 "github.com/davidmovas/chain-forge/chain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type BlockApplier interface {
	ApplyBlockToState(blk chain2.SigBlock) error
}

type BlockRelayer interface {
	RelayBlock(blk chain2.SigBlock)
}

type BlockSrv struct {
	UnimplementedBlockServer
	blockStoreDir string
	eventPub      chain2.EventPublisher
	blkApplier    BlockApplier
	blkRelayer    BlockRelayer
}

func NewBlockSrv(
	blockStoreDir string, eventPub chain2.EventPublisher,
	blkApplier BlockApplier, blkRelayer BlockRelayer,
) *BlockSrv {
	return &BlockSrv{
		blockStoreDir: blockStoreDir, eventPub: eventPub,
		blkApplier: blkApplier, blkRelayer: blkRelayer,
	}
}

func (s *BlockSrv) GenesisSync(
	_ context.Context, req *GenesisSyncReq,
) (*GenesisSyncRes, error) {
	jgen, err := chain2.ReadGenesisBytes(s.blockStoreDir)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}
	res := &GenesisSyncRes{Genesis: jgen}
	return res, nil
}

func (s *BlockSrv) BlockSync(
	req *BlockSyncReq, stream grpc.ServerStreamingServer[BlockSyncRes],
) error {
	blocks, closeBlocks, err := chain2.ReadBlocksBytes(s.blockStoreDir)
	if err != nil {
		return status.Errorf(codes.NotFound, err.Error())
	}
	defer closeBlocks()
	num, i := int(req.Number), 1
	for err, jblk := range blocks {
		if err != nil {
			return status.Errorf(codes.Internal, err.Error())
		}
		if i >= num {
			res := &BlockSyncRes{Block: jblk}
			err = stream.Send(res)
			if err != nil {
				return status.Errorf(codes.Internal, err.Error())
			}
		}
		i++
	}
	return nil
}

func (s *BlockSrv) publishBlockAndTxs(blk chain2.SigBlock) {
	jblk, _ := json.Marshal(blk)
	event := chain2.NewEvent(chain2.EvBlock, "validated", jblk)
	s.eventPub.PublishEvent(event)
	for _, tx := range blk.Txs {
		jtx, _ := json.Marshal(tx)
		event := chain2.NewEvent(chain2.EvTx, "validated", jtx)
		s.eventPub.PublishEvent(event)
	}
}

func (s *BlockSrv) BlockReceive(
	stream grpc.ClientStreamingServer[BlockReceiveReq, BlockReceiveRes],
) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			res := &BlockReceiveRes{}
			return stream.SendAndClose(res)
		}
		if err != nil {
			return status.Errorf(codes.Internal, err.Error())
		}
		var blk chain2.SigBlock
		err = json.Unmarshal(req.Block, &blk)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Printf("<== Block receive\n%v", blk)
		err = s.blkApplier.ApplyBlockToState(blk)
		if err != nil {
			fmt.Print(err)
			continue
		}
		err = blk.Write(s.blockStoreDir)
		if err != nil {
			fmt.Println(err)
			continue
		}
		if s.blkRelayer != nil {
			s.blkRelayer.RelayBlock(blk)
		}
		if s.eventPub != nil {
			s.publishBlockAndTxs(blk)
		}
	}
}

func (s *BlockSrv) BlockSearch(
	req *BlockSearchReq, stream grpc.ServerStreamingServer[BlockSearchRes],
) error {
	blocks, closeBlocks, err := chain2.ReadBlocks(s.blockStoreDir)
	if err != nil {
		return status.Errorf(codes.NotFound, err.Error())
	}
	defer closeBlocks()
	prefix := strings.HasPrefix
	for err, blk := range blocks {
		if err != nil {
			return status.Errorf(codes.Internal, err.Error())
		}
		if req.Number != 0 && blk.Number == req.Number ||
			len(req.Hash) > 0 && prefix(blk.Hash().String(), req.Hash) ||
			len(req.Parent) > 0 && prefix(blk.Parent.String(), req.Parent) {
			jblk, err := json.Marshal(blk)
			if err != nil {
				return status.Errorf(codes.Internal, err.Error())
			}
			res := &BlockSearchRes{Block: jblk}
			err = stream.Send(res)
			if err != nil {
				return status.Errorf(codes.Internal, err.Error())
			}
			break
		}
	}
	return nil
}
