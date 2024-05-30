package sequencerapi

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	txConditionalMaxCost = 1000
)

var (
	sendRawTxConditionalCostMeter = metrics.NewRegisteredMeter("sequencer/sendRawTransactionConditional/cost", nil)

	sendRawTxConditionalRequestsCounter = metrics.NewRegisteredCounter("sequencer/sendRawTransactionConditional/requests", nil)
	sendRawTxConditionalAcceptedCounter = metrics.NewRegisteredCounter("sequencer/sendRawTransactionConditional/accepted", nil)
)

type ethApi struct {
	b ethapi.Backend
}

func GetAPIs(b ethapi.Backend) []rpc.API {
	return []rpc.API{
		{
			Namespace: "eth",
			Service:   &ethApi{b},
		},
	}
}

func (s *ethApi) SendRawTransactionConditional(ctx context.Context, txBytes hexutil.Bytes, cond types.TransactionConditional) (common.Hash, error) {
	sendRawTxConditionalRequestsCounter.Inc(1)

	cost := cond.Cost()
	sendRawTxConditionalCostMeter.Mark(int64(cost))
	if cost > txConditionalMaxCost {
		return common.Hash{}, fmt.Errorf("conditional cost, %d, exceeded 1000", cost)
	}

	state, header, err := s.b.StateAndHeaderByNumber(context.Background(), rpc.LatestBlockNumber)
	if err != nil {
		return common.Hash{}, err
	}
	if header.CheckTransactionConditional(&cond); err != nil {
		return common.Hash{}, fmt.Errorf("failed header check: %w", err)
	}
	if state.CheckTransactionConditional(&cond); err != nil {
		return common.Hash{}, fmt.Errorf("failed state check: %w", err)
	}

	// We also check against the parent block to eliminate the MEV incentive in comparison with sendRawTransaction
	parentBlock := rpc.BlockNumberOrHash{BlockHash: &header.ParentHash}
	parentState, _, err := s.b.StateAndHeaderByNumberOrHash(context.Background(), parentBlock)
	if err != nil {
		return common.Hash{}, err
	}
	if parentState.CheckTransactionConditional(&cond); err != nil {
		return common.Hash{}, fmt.Errorf("failed parent header state check: %w", err)
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		return common.Hash{}, err
	}

	sendRawTxConditionalAcceptedCounter.Inc(1)

	tx.SetConditional(&cond)
	return ethapi.SubmitTransaction(ctx, s.b, tx)
}
