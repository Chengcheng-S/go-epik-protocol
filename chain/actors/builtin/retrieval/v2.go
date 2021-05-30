package retrieval

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
	retrieval2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/retrieval"
	"github.com/filecoin-project/specs-actors/v2/actors/util/adt"
	"github.com/ipfs/go-cid"
)

var _ State = (*state)(nil)

func load2(store adt.Store, root cid.Cid) (State, error) {
	out := state{store: store}
	err := store.Get(store.Context(), root, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type state struct {
	retrieval2.State
	store adt.Store
}

func (s *state) StateInfo(fromAddr address.Address) (*RetrievalState, error) {
	info, err := s.State.StateInfo(s.store, fromAddr)
	if err != nil {
		return nil, err
	}
	mmap, err := adt.AsMap(s.store, info.Miners, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, err
	}
	miners, err := mmap.CollectKeys()
	if err != nil {
		return nil, err
	}
	var addrs []address.Address
	for _, miner := range miners {
		addr, err := address.NewFromString(miner)
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	return &RetrievalState{
		BindMiners: addrs,
		Amount:     info.Amount,
		EpochDate:  info.EpochDate,
		DateSize:   info.DailyDataSize,
	}, nil
}

func (s *state) DayExpend(epoch abi.ChainEpoch, fromAddr address.Address) (abi.TokenAmount, error) {
	return s.State.DayExpend(s.store, epoch, fromAddr)
}

func (s *state) LockedState(fromAddr address.Address) (*LockedState, error) {
	return s.State.LockedState(s.store, fromAddr)
}

func (s *state) TotalCollateral() (abi.TokenAmount, error) {
	return s.State.TotalCollateral, nil
}

func (s *state) TotalRetrievalReward() (abi.TokenAmount, error) {
	return s.State.TotalRetrievalReward, nil
}

func (s *state) PendingReward() (abi.TokenAmount, error) {
	return s.State.PendingReward, nil
}
