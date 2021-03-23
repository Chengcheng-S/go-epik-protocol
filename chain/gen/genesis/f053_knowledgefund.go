package genesis

import (
	"context"

	bstore "github.com/EpiK-Protocol/go-epik/blockstore"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin/knowledge"
	"github.com/filecoin-project/specs-actors/v2/actors/util/adt"
	cbor "github.com/ipfs/go-ipld-cbor"
)

func SetupKnowledgeActor(bs bstore.Blockstore, initialPayee address.Address) (*types.Actor, error) {
	store := adt.WrapStore(context.TODO(), cbor.NewCborStore(bs))

	kas, err := knowledge.ConstructState(store, initialPayee)
	if err != nil {
		return nil, err
	}

	stcid, err := store.Put(store.Context(), kas)
	if err != nil {
		return nil, err
	}

	return &types.Actor{
		Code:    builtin.KnowledgeFundActorCodeID,
		Head:    stcid,
		Nonce:   0,
		Balance: types.NewInt(0),
	}, nil
}
