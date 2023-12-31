package mock

import (
	"io"

	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/flowch"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
)

type mockState struct {
	from       address.Address
	to         address.Address
	settlingAt abi.ChainEpoch
	toSend     abi.TokenAmount
	lanes      map[uint64]flowch.LaneState
}

type mockLaneState struct {
	redeemed big.Int
	nonce    uint64
}

// NewMockFlowChState constructs a state for a flow channel with the set fixed values
// that satisfies the flowch.State interface.
func NewMockFlowChState(from address.Address,
	to address.Address,
	settlingAt abi.ChainEpoch,
	lanes map[uint64]flowch.LaneState,
) flowch.State {
	return &mockState{from: from, to: to, settlingAt: settlingAt, toSend: big.NewInt(0), lanes: lanes}
}

// NewMockLaneState constructs a state for a payment channel lane with the set fixed values
// that satisfies the flowch.LaneState interface. Useful for populating lanes when
// calling NewMockflowchState
func NewMockLaneState(redeemed big.Int, nonce uint64) flowch.LaneState {
	return &mockLaneState{redeemed, nonce}
}

func (ms *mockState) MarshalCBOR(io.Writer) error {
	panic("not implemented")
}

// Channel owner, who has funded the actor
func (ms *mockState) From() (address.Address, error) {
	return ms.from, nil
}

// Recipient of payouts from channel
func (ms *mockState) To() (address.Address, error) {
	return ms.to, nil
}

// Height at which the channel can be `Collected`
func (ms *mockState) SettlingAt() (abi.ChainEpoch, error) {
	return ms.settlingAt, nil
}

// Amount successfully redeemed through the payment channel, paid out on `Collect()`
func (ms *mockState) ToSend() (abi.TokenAmount, error) {
	return ms.toSend, nil
}

// Get total number of lanes
func (ms *mockState) LaneCount() (uint64, error) {
	return uint64(len(ms.lanes)), nil
}

func (ms *mockState) Received() (abi.TokenAmount, error) {
	return abi.NewTokenAmount(0), nil
}

// Iterate lane states
func (ms *mockState) ForEachLaneState(cb func(idx uint64, dl flowch.LaneState) error) error {
	var lastErr error
	for lane, state := range ms.lanes {
		if err := cb(lane, state); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (mls *mockLaneState) Redeemed() (big.Int, error) {
	return mls.redeemed, nil
}

func (mls *mockLaneState) Nonce() (uint64, error) {
	return mls.nonce, nil
}
