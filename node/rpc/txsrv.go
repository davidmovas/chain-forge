package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	chain2 "github.com/davidmovas/chain-forge/chain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type TxApplier interface {
	Nonce(acc chain2.Address) uint64
	ApplyTx(tx chain2.SigTx) error
}

type TxRelayer interface {
	RelayTx(tx chain2.SigTx)
}

type TxSrv struct {
	UnimplementedTxServer
	keyStoreDir   string
	blockStoreDir string
	txApplier     TxApplier
	txRelayer     TxRelayer
}

func NewTxSrv(
	keyStoreDir string, blockStoreDir string,
	txApplier TxApplier, txRelayer TxRelayer,
) *TxSrv {
	return &TxSrv{
		keyStoreDir: keyStoreDir, blockStoreDir: blockStoreDir,
		txApplier: txApplier, txRelayer: txRelayer,
	}
}

func (s *TxSrv) TxSign(_ context.Context, req *TxSignReq) (*TxSignRes, error) {
	path := filepath.Join(s.keyStoreDir, req.From)
	acc, err := chain2.ReadAccount(path, []byte(req.Password))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}
	tx := chain2.NewTx(
		chain2.Address(req.From), chain2.Address(req.To), req.Value,
		s.txApplier.Nonce(chain2.Address(req.From))+1,
	)
	stx, err := acc.SignTx(tx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}
	jtx, err := json.Marshal(stx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}
	res := &TxSignRes{Tx: jtx}
	return res, nil
}

func (s *TxSrv) TxSend(_ context.Context, req *TxSendReq) (*TxSendRes, error) {
	var tx chain2.SigTx
	err := json.Unmarshal(req.Tx, &tx)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}
	err = s.txApplier.ApplyTx(tx)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, err.Error())
	}
	if s.txRelayer != nil {
		s.txRelayer.RelayTx(tx)
	}
	res := &TxSendRes{Hash: tx.Hash().String()}
	return res, nil
}

func (s *TxSrv) TxReceive(
	stream grpc.ClientStreamingServer[TxReceiveReq, TxReceiveRes],
) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			res := &TxReceiveRes{}
			return stream.SendAndClose(res)
		}
		if err != nil {
			return status.Errorf(codes.Internal, err.Error())
		}
		var tx chain2.SigTx
		err = json.Unmarshal(req.Tx, &tx)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Printf("<== Tx receive\n%v\n", tx)
		err = s.txApplier.ApplyTx(tx)
		if err != nil {
			fmt.Print(err)
			continue
		}
		if s.txRelayer != nil {
			s.txRelayer.RelayTx(tx)
		}
	}
}

func sendTxSearchRes(
	blk chain2.SigBlock, tx chain2.SigTx,
	stream grpc.ServerStreamingServer[TxSearchRes],
) error {
	stx := chain2.NewSearchTx(tx, blk.Number, blk.Hash(), blk.MerkleRoot)
	jtx, err := json.Marshal(stx)
	if err != nil {
		return err
	}
	res := &TxSearchRes{Tx: jtx}
	err = stream.Send(res)
	if err != nil {
		return err
	}
	return nil
}

func (s *TxSrv) TxSearch(
	req *TxSearchReq, stream grpc.ServerStreamingServer[TxSearchRes],
) error {
	blocks, closeBlocks, err := chain2.ReadBlocks(s.blockStoreDir)
	if err != nil {
		return status.Errorf(codes.NotFound, err.Error())
	}
	defer closeBlocks()
	prefix := strings.HasPrefix
block:
	for err, blk := range blocks {
		if err != nil {
			return status.Errorf(codes.Internal, err.Error())
		}
		for _, tx := range blk.Txs {
			if len(req.Hash) > 0 && prefix(tx.Hash().String(), req.Hash) {
				err = sendTxSearchRes(blk, tx, stream)
				if err != nil {
					return status.Errorf(codes.Internal, err.Error())
				}
				break block
			}
			if len(req.From) > 0 && prefix(string(tx.From), req.From) ||
				len(req.To) > 0 && prefix(string(tx.To), req.To) ||
				len(req.Account) > 0 &&
					(prefix(string(tx.From), req.From) || prefix(string(tx.To), req.To)) {
				err := sendTxSearchRes(blk, tx, stream)
				if err != nil {
					return status.Errorf(codes.Internal, err.Error())
				}
			}
		}
	}
	return nil
}

func (s *TxSrv) TxProve(
	_ context.Context, req *TxProveReq,
) (*TxProveRes, error) {
	blocks, closeBlocks, err := chain2.ReadBlocks(s.blockStoreDir)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}
	defer closeBlocks()
	for err, blk := range blocks {
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
		for _, tx := range blk.Txs {
			if tx.Hash().String() == req.Hash {
				merkleTree, err := chain2.MerkleHash(
					blk.Txs, chain2.TxHash, chain2.TxPairHash,
				)
				if err != nil {
					return nil, status.Errorf(codes.Internal, err.Error())
				}
				merkleProof, err := chain2.MerkleProve(tx.Hash(), merkleTree)
				if err != nil {
					return nil, status.Errorf(codes.Internal, err.Error())
				}
				jmp, err := json.Marshal(merkleProof)
				if err != nil {
					return nil, status.Errorf(codes.Internal, err.Error())
				}
				res := &TxProveRes{MerkleProof: jmp}
				return res, nil
			}
		}
	}
	return nil, status.Errorf(
		codes.NotFound, fmt.Sprintf("transaction %v not found", req.Hash),
	)
}

func (s *TxSrv) TxVerify(
	_ context.Context, req *TxVerifyReq,
) (*TxVerifyRes, error) {
	txh, err := chain2.DecodeHash(req.Hash)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}
	var merkleProof []chain2.Proof[chain2.Hash]
	err = json.Unmarshal(req.MerkleProof, &merkleProof)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}
	merkleRoot, err := chain2.DecodeHash(req.MerkleRoot)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}
	valid := chain2.MerkleVerify(txh, merkleProof, merkleRoot, chain2.TxPairHash)
	res := &TxVerifyRes{Valid: valid}
	return res, nil
}
