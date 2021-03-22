package account

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/ipfs/go-cid"

	"github.com/EpiK-Protocol/go-epik/chain/actors/adt"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin"
	"github.com/EpiK-Protocol/go-epik/chain/types"

	builtin3 "github.com/filecoin-project/specs-actors/v2/actors/builtin"
)

func init() {
	// builtin.RegisterActorState(builtin2.AccountActorCodeID, func(store adt.Store, root cid.Cid) (cbor.Marshaler, error) {
	// 	return load2(store, root)
	// })
	builtin.RegisterActorState(builtin3.AccountActorCodeID, func(store adt.Store, root cid.Cid) (cbor.Marshaler, error) {
		return load3(store, root)
	})
}

var Methods = builtin3.MethodsAccount

func Load(store adt.Store, act *types.Actor) (State, error) {
	switch act.Code {
	case builtin3.AccountActorCodeID:
		return load3(store, act.Head)
	}
	return nil, xerrors.Errorf("unknown actor code %s", act.Code)
}

type State interface {
	cbor.Marshaler

	PubkeyAddress() (address.Address, error)
}
