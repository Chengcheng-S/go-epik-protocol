package settler

import (
	"context"
	"sync"

	"github.com/EpiK-Protocol/go-epik/flowchmgr"

	"go.uber.org/fx"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/EpiK-Protocol/go-epik/api"
	"github.com/EpiK-Protocol/go-epik/build"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/flowch"
	"github.com/EpiK-Protocol/go-epik/chain/events"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	flowapi "github.com/EpiK-Protocol/go-epik/node/impl/flowch"
	"github.com/EpiK-Protocol/go-epik/node/impl/full"
	"github.com/EpiK-Protocol/go-epik/node/modules/helpers"
)

var log = logging.Logger("payment-channel-settler")

// API are the dependencies need to run the payment channel settler
type API struct {
	fx.In

	full.ChainAPI
	full.StateAPI
	flowapi.FlowchAPI
}

type settlerAPI interface {
	FlowchList(context.Context) ([]address.Address, error)
	FlowchStatus(context.Context, address.Address) (*api.FlowchStatus, error)
	FlowchVoucherCheckSpendable(context.Context, address.Address, *flowch.SignedVoucher, []byte, []byte) (bool, error)
	FlowchVoucherList(context.Context, address.Address) ([]*flowch.SignedVoucher, error)
	FlowchVoucherSubmit(context.Context, address.Address, *flowch.SignedVoucher, []byte, []byte) (cid.Cid, error)
	StateWaitMsg(ctx context.Context, cid cid.Cid, confidence uint64) (*api.MsgLookup, error)
}

type paymentChannelSettler struct {
	ctx context.Context
	api settlerAPI
}

// SettlePaymentChannels checks the chain for events related to payment channels settling and
// submits any vouchers for inbound channels tracked for this node
func SettlePaymentChannels(mctx helpers.MetricsCtx, lc fx.Lifecycle, api API) error {
	ctx := helpers.LifecycleCtx(mctx, lc)
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			pcs := newPaymentChannelSettler(ctx, &api)
			ev := events.NewEvents(ctx, &api)
			return ev.Called(pcs.check, pcs.messageHandler, pcs.revertHandler, int(build.MessageConfidence+1), events.NoTimeout, pcs.matcher)
		},
	})
	return nil
}

func newPaymentChannelSettler(ctx context.Context, api settlerAPI) *paymentChannelSettler {
	return &paymentChannelSettler{
		ctx: ctx,
		api: api,
	}
}

func (pcs *paymentChannelSettler) check(ts *types.TipSet) (done bool, more bool, err error) {
	return false, true, nil
}

func (pcs *paymentChannelSettler) messageHandler(msg *types.Message, rec *types.MessageReceipt, ts *types.TipSet, curH abi.ChainEpoch) (more bool, err error) {
	// Ignore unsuccessful settle messages
	if rec.ExitCode != 0 {
		return true, nil
	}

	bestByLane, err := flowchmgr.BestSpendableByLane(pcs.ctx, pcs.api, msg.To)
	if err != nil {
		return true, err
	}
	var wg sync.WaitGroup
	wg.Add(len(bestByLane))
	for _, voucher := range bestByLane {
		submitMessageCID, err := pcs.api.FlowchVoucherSubmit(pcs.ctx, msg.To, voucher, nil, nil)
		if err != nil {
			return true, err
		}
		go func(voucher *flowch.SignedVoucher, submitMessageCID cid.Cid) {
			defer wg.Done()
			msgLookup, err := pcs.api.StateWaitMsg(pcs.ctx, submitMessageCID, build.MessageConfidence)
			if err != nil {
				log.Errorf("submitting voucher: %s", err.Error())
			}
			if msgLookup.Receipt.ExitCode != 0 {
				log.Errorf("failed submitting voucher: %+v", voucher)
			}
		}(voucher, submitMessageCID)
	}
	wg.Wait()
	return true, nil
}

func (pcs *paymentChannelSettler) revertHandler(ctx context.Context, ts *types.TipSet) error {
	return nil
}

func (pcs *paymentChannelSettler) matcher(msg *types.Message, ts *types.TipSet) (matched bool, err error) {
	// Check if this is a settle payment channel message
	if msg.Method != flowch.Methods.Settle {
		return false, nil
	}
	// Check if this payment channel is of concern to this node (i.e. tracked in payment channel store),
	// and its inbound (i.e. we're getting vouchers that we may need to redeem)
	trackedAddresses, err := pcs.api.FlowchList(pcs.ctx)
	if err != nil {
		return false, err
	}
	for _, addr := range trackedAddresses {
		if msg.To == addr {
			status, err := pcs.api.FlowchStatus(pcs.ctx, addr)
			if err != nil {
				return false, err
			}
			if status.Direction == api.PCHInbound {
				return true, nil
			}
		}
	}
	return false, nil
}
