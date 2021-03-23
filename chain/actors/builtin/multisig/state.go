package multisig

import (
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/ipfs/go-cid"

	builtin3 "github.com/filecoin-project/specs-actors/v2/actors/builtin"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin/multisig"

	"github.com/EpiK-Protocol/go-epik/chain/actors/adt"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin"
	"github.com/EpiK-Protocol/go-epik/chain/types"
)

func init() {
	builtin.RegisterActorState(builtin3.MultisigActorCodeID, func(store adt.Store, root cid.Cid) (cbor.Marshaler, error) {
		return load3(store, root)
	})
}

func Load(store adt.Store, act *types.Actor) (State, error) {
	switch act.Code {
	case builtin3.MultisigActorCodeID:
		return load3(store, act.Head)
	}
	return nil, xerrors.Errorf("unknown actor code %s", act.Code)
}

type State interface {
	cbor.Marshaler

	LockedBalance(epoch abi.ChainEpoch) (abi.TokenAmount, error)
	StartEpoch() (abi.ChainEpoch, error)
	UnlockDuration() (abi.ChainEpoch, error)
	InitialBalance() (abi.TokenAmount, error)
	Threshold() (uint64, error)
	Signers() ([]address.Address, error)

	ForEachPendingTxn(func(id int64, txn Transaction) error) error
	PendingTxnChanged(State) (bool, error)
	PendingTxn(id int64) (Transaction, error)

	transactions() (adt.Map, error)
	decodeTransaction(val *cbg.Deferred) (Transaction, error)
}

type Transaction = multisig.Transaction
