package test

/* import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/EpiK-Protocol/go-epik/api"
	"github.com/EpiK-Protocol/go-epik/build"
	"github.com/EpiK-Protocol/go-epik/chain/stmgr"
	sealing "github.com/EpiK-Protocol/go-epik/extern/storage-sealing"
	"github.com/EpiK-Protocol/go-epik/node"
	"github.com/EpiK-Protocol/go-epik/node/impl"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/stretchr/testify/require"
)

func TestTapeFix(t *testing.T, b APIBuilder, blocktime time.Duration) {
	// The "before" case is disabled, because we need the builder to mock 32 GiB sectors to accurately repro this case
	// TODO: Make the mock sector size configurable and reenable this
	//t.Run("before", func(t *testing.T) { testTapeFix(t, b, blocktime, false) })
	t.Run("after", func(t *testing.T) { testTapeFix(t, b, blocktime, true) })
}
func testTapeFix(t *testing.T, b APIBuilder, blocktime time.Duration, after bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	upgradeSchedule := stmgr.UpgradeSchedule{{
		// Network:   build.ActorUpgradeNetworkVersion,
		Network:   build.NewestNetworkVersion,
		Height:    1,
		Migration: stmgr.UpgradeActorsV2,
	}}
	if after {
		upgradeSchedule = append(upgradeSchedule, stmgr.Upgrade{
			Network: network.Version5,
			Height:  2,
		})
	}

	n, sn := b(t, []FullNodeOpts{{Opts: func(_ []TestNode) node.Option {
		return node.Override(new(stmgr.UpgradeSchedule), upgradeSchedule)
	}}}, OneMiner)

	client := n[0].FullNode.(*impl.FullNodeAPI)
	miner := sn[0]

	addrinfo, err := client.NetAddrsListen(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := miner.NetConnect(ctx, addrinfo); err != nil {
		t.Fatal(err)
	}
	build.Clock.Sleep(time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			build.Clock.Sleep(blocktime)
			if err := sn[0].MineOne(ctx, MineNext); err != nil {
				if ctx.Err() != nil {
					// context was canceled, ignore the error.
					return
				}
				t.Error(err)
			}
		}
	}()
	defer func() {
		cancel()
		<-done
	}()

	sid, err := miner.PledgeSector(ctx)
	require.NoError(t, err)

	fmt.Printf("All sectors is fsm\n")

	// If before, we expect the precommit to fail
	successState := api.SectorState(sealing.CommitFailed)
	failureState := api.SectorState(sealing.Proving)
	if after {
		// otherwise, it should succeed.
		successState, failureState = failureState, successState
	}

	for {
		st, err := miner.SectorsStatus(ctx, sid.Number, false)
		require.NoError(t, err)
		if st.State == successState {
			break
		}
		require.NotEqual(t, failureState, st.State)
		build.Clock.Sleep(100 * time.Millisecond)
		fmt.Println("WaitSeal")
	}

} */
