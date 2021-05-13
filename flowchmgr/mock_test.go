package flowchmgr

import (
	"context"
	"errors"
	"sync"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/EpiK-Protocol/go-epik/api"
	"github.com/EpiK-Protocol/go-epik/build"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/flowch"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/lib/sigs"
)

type mockManagerAPI struct {
	*mockStateManager
	*mockFlowchAPI
}

func newMockManagerAPI() *mockManagerAPI {
	return &mockManagerAPI{
		mockStateManager: newMockStateManager(),
		mockFlowchAPI:    newMockFlowchAPI(),
	}
}

type mockPchState struct {
	actor *types.Actor
	state flowch.State
}

type mockStateManager struct {
	lk           sync.Mutex
	accountState map[address.Address]address.Address
	flowchState  map[address.Address]mockPchState
	response     *api.InvocResult
	lastCall     *types.Message
}

func newMockStateManager() *mockStateManager {
	return &mockStateManager{
		accountState: make(map[address.Address]address.Address),
		flowchState:  make(map[address.Address]mockPchState),
	}
}

func (sm *mockStateManager) setAccountAddress(a address.Address, lookup address.Address) {
	sm.lk.Lock()
	defer sm.lk.Unlock()
	sm.accountState[a] = lookup
}

func (sm *mockStateManager) setFlowchState(a address.Address, actor *types.Actor, state flowch.State) {
	sm.lk.Lock()
	defer sm.lk.Unlock()
	sm.flowchState[a] = mockPchState{actor, state}
}

func (sm *mockStateManager) ResolveToKeyAddress(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error) {
	sm.lk.Lock()
	defer sm.lk.Unlock()
	keyAddr, ok := sm.accountState[addr]
	if !ok {
		return address.Undef, errors.New("not found")
	}
	return keyAddr, nil
}

func (sm *mockStateManager) GetFlowchState(ctx context.Context, addr address.Address, ts *types.TipSet) (*types.Actor, flowch.State, error) {
	sm.lk.Lock()
	defer sm.lk.Unlock()
	info, ok := sm.flowchState[addr]
	if !ok {
		return nil, nil, errors.New("not found")
	}
	return info.actor, info.state, nil
}

func (sm *mockStateManager) setCallResponse(response *api.InvocResult) {
	sm.lk.Lock()
	defer sm.lk.Unlock()

	sm.response = response
}

func (sm *mockStateManager) getLastCall() *types.Message {
	sm.lk.Lock()
	defer sm.lk.Unlock()

	return sm.lastCall
}

func (sm *mockStateManager) Call(ctx context.Context, msg *types.Message, ts *types.TipSet) (*api.InvocResult, error) {
	sm.lk.Lock()
	defer sm.lk.Unlock()

	sm.lastCall = msg

	return sm.response, nil
}

type waitingCall struct {
	response chan types.MessageReceipt
}

type waitingResponse struct {
	receipt types.MessageReceipt
	done    chan struct{}
}

type mockFlowchAPI struct {
	lk               sync.Mutex
	messages         map[cid.Cid]*types.SignedMessage
	waitingCalls     map[cid.Cid]*waitingCall
	waitingResponses map[cid.Cid]*waitingResponse
	wallet           map[address.Address]struct{}
	signingKey       []byte
}

func newMockFlowchAPI() *mockFlowchAPI {
	return &mockFlowchAPI{
		messages:         make(map[cid.Cid]*types.SignedMessage),
		waitingCalls:     make(map[cid.Cid]*waitingCall),
		waitingResponses: make(map[cid.Cid]*waitingResponse),
		wallet:           make(map[address.Address]struct{}),
	}
}

func (pchapi *mockFlowchAPI) StateWaitMsg(ctx context.Context, mcid cid.Cid, confidence uint64) (*api.MsgLookup, error) {
	pchapi.lk.Lock()

	response := make(chan types.MessageReceipt)

	if response, ok := pchapi.waitingResponses[mcid]; ok {
		defer pchapi.lk.Unlock()
		defer func() {
			go close(response.done)
		}()

		delete(pchapi.waitingResponses, mcid)
		return &api.MsgLookup{Receipt: response.receipt}, nil
	}

	pchapi.waitingCalls[mcid] = &waitingCall{response: response}
	pchapi.lk.Unlock()

	receipt := <-response
	return &api.MsgLookup{Receipt: receipt}, nil
}

func (pchapi *mockFlowchAPI) receiveMsgResponse(mcid cid.Cid, receipt types.MessageReceipt) {
	pchapi.lk.Lock()

	if call, ok := pchapi.waitingCalls[mcid]; ok {
		defer pchapi.lk.Unlock()

		delete(pchapi.waitingCalls, mcid)
		call.response <- receipt
		return
	}

	done := make(chan struct{})
	pchapi.waitingResponses[mcid] = &waitingResponse{receipt: receipt, done: done}

	pchapi.lk.Unlock()

	<-done
}

// Send success response for any waiting calls
func (pchapi *mockFlowchAPI) close() {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	success := types.MessageReceipt{
		ExitCode: 0,
		Return:   []byte{},
	}
	for mcid, call := range pchapi.waitingCalls {
		delete(pchapi.waitingCalls, mcid)
		call.response <- success
	}
}

func (pchapi *mockFlowchAPI) MpoolPushMessage(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec) (*types.SignedMessage, error) {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	smsg := &types.SignedMessage{Message: *msg}
	pchapi.messages[smsg.Cid()] = smsg
	return smsg, nil
}

func (pchapi *mockFlowchAPI) pushedMessages(c cid.Cid) *types.SignedMessage {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	return pchapi.messages[c]
}

func (pchapi *mockFlowchAPI) pushedMessageCount() int {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	return len(pchapi.messages)
}

func (pchapi *mockFlowchAPI) StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error) {
	return addr, nil
}

func (pchapi *mockFlowchAPI) WalletHas(ctx context.Context, addr address.Address) (bool, error) {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	_, ok := pchapi.wallet[addr]
	return ok, nil
}

func (pchapi *mockFlowchAPI) addWalletAddress(addr address.Address) {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	pchapi.wallet[addr] = struct{}{}
}

func (pchapi *mockFlowchAPI) WalletSign(ctx context.Context, k address.Address, msg []byte) (*crypto.Signature, error) {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	return sigs.Sign(crypto.SigTypeSecp256k1, pchapi.signingKey, msg)
}

func (pchapi *mockFlowchAPI) addSigningKey(key []byte) {
	pchapi.lk.Lock()
	defer pchapi.lk.Unlock()

	pchapi.signingKey = key
}

func (pchapi *mockFlowchAPI) StateNetworkVersion(ctx context.Context, tsk types.TipSetKey) (network.Version, error) {
	return build.NewestNetworkVersion, nil
}
