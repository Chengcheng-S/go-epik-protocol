package modules

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/EpiK-Protocol/go-epik/api"
	"github.com/EpiK-Protocol/go-epik/blockstore"
	"github.com/EpiK-Protocol/go-epik/chain/actors/adt"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/flowch"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/paych"
	"github.com/EpiK-Protocol/go-epik/chain/stmgr"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/filecoin-project/go-address"
	cbor "github.com/ipfs/go-ipld-cbor"
)

type RPCStateManager struct {
	gapi   api.GatewayAPI
	cstore *cbor.BasicIpldStore
}

func NewRPCStateManager(api api.GatewayAPI) *RPCStateManager {
	cstore := cbor.NewCborStore(blockstore.NewAPIBlockstore(api))
	return &RPCStateManager{gapi: api, cstore: cstore}
}

func (s *RPCStateManager) GetPaychState(ctx context.Context, addr address.Address, ts *types.TipSet) (*types.Actor, paych.State, error) {
	act, err := s.gapi.StateGetActor(ctx, addr, ts.Key())
	if err != nil {
		return nil, nil, err
	}

	actState, err := paych.Load(adt.WrapStore(ctx, s.cstore), act)
	if err != nil {
		return nil, nil, err
	}
	return act, actState, nil

}

func (s *RPCStateManager) GetFlowchState(ctx context.Context, addr address.Address, ts *types.TipSet) (*types.Actor, flowch.State, error) {
	act, err := s.gapi.StateGetActor(ctx, addr, ts.Key())
	if err != nil {
		return nil, nil, err
	}

	actState, err := flowch.Load(adt.WrapStore(ctx, s.cstore), act)
	if err != nil {
		return nil, nil, err
	}
	return act, actState, nil

}

func (s *RPCStateManager) LoadActorTsk(ctx context.Context, addr address.Address, tsk types.TipSetKey) (*types.Actor, error) {
	return s.gapi.StateGetActor(ctx, addr, tsk)
}

func (s *RPCStateManager) LookupID(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error) {
	return s.gapi.StateLookupID(ctx, addr, ts.Key())
}

func (s *RPCStateManager) ResolveToKeyAddress(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error) {
	return s.gapi.StateAccountKey(ctx, addr, ts.Key())
}

func (s *RPCStateManager) Call(ctx context.Context, msg *types.Message, ts *types.TipSet) (*api.InvocResult, error) {
	return nil, xerrors.Errorf("RPCStateManager does not implement StateManager.Call")
}

var _ stmgr.StateManagerAPI = (*RPCStateManager)(nil)
