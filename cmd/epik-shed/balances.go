package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/EpiK-Protocol/go-epik/chain/gen/genesis"

	_init "github.com/EpiK-Protocol/go-epik/chain/actors/builtin/init"

	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/multisig"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/EpiK-Protocol/go-epik/chain/actors/adt"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/miner"
	"github.com/EpiK-Protocol/go-epik/chain/state"
	"github.com/EpiK-Protocol/go-epik/chain/stmgr"
	"github.com/EpiK-Protocol/go-epik/chain/store"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/chain/vm"
	lcli "github.com/EpiK-Protocol/go-epik/cli"
	"github.com/EpiK-Protocol/go-epik/extern/sector-storage/ffiwrapper"
	"github.com/EpiK-Protocol/go-epik/node/repo"
)

type accountInfo struct {
	Address  address.Address
	Balance  types.EPK
	Type     string
	Power    abi.StoragePower
	Worker   address.Address
	Owner    address.Address
	Coinbase address.Address
	/* InitialPledge   types.EPK
	PreCommits      types.EPK */
	LockedFunds     types.EPK
	Sectors         uint64
	VestingStart    abi.ChainEpoch
	VestingDuration abi.ChainEpoch
	VestingAmount   types.EPK
}

var auditsCmd = &cli.Command{
	Name:        "audits",
	Description: "a collection of utilities for auditing the epik chain",
	Subcommands: []*cli.Command{
		chainBalanceCmd,
		chainBalanceStateCmd,
		// chainPledgeCmd,
		fillBalancesCmd,
	},
}

var chainBalanceCmd = &cli.Command{
	Name:        "chain-balances",
	Description: "Produces a csv file of all account balances",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "tipset",
			Usage: "specify tipset to start from",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}

		defer closer()
		ctx := lcli.ReqContext(cctx)

		ts, err := lcli.LoadTipSet(ctx, cctx, api)
		if err != nil {
			return err
		}

		tsk := ts.Key()
		actors, err := api.StateListActors(ctx, tsk)
		if err != nil {
			return err
		}

		var infos []accountInfo
		for _, addr := range actors {
			act, err := api.StateGetActor(ctx, addr, tsk)
			if err != nil {
				return err
			}

			ai := accountInfo{
				Address: addr,
				Balance: types.EPK(act.Balance),
				Type:    string(act.Code.Hash()[2:]),
			}

			if builtin.IsStorageMinerActor(act.Code) {
				pow, err := api.StateMinerPower(ctx, addr, tsk)
				if err != nil {
					return xerrors.Errorf("failed to get power: %w", err)
				}

				ai.Power = pow.MinerPower.RawBytePower
				info, err := api.StateMinerInfo(ctx, addr, tsk)
				if err != nil {
					return xerrors.Errorf("failed to get miner info: %w", err)
				}
				ai.Worker = info.Worker
				ai.Owner = info.Owner
				ai.Coinbase = info.Coinbase

			}
			infos = append(infos, ai)
		}

		printAccountInfos(infos, false)

		return nil
	},
}

var chainBalanceStateCmd = &cli.Command{
	Name:        "stateroot-balances",
	Description: "Produces a csv file of all account balances from a given stateroot",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "repo",
			Value: "~/.epik",
		},
		&cli.BoolFlag{
			Name: "miner-info",
		},
		&cli.BoolFlag{
			Name: "robust-addresses",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		if !cctx.Args().Present() {
			return fmt.Errorf("must pass state root")
		}

		sroot, err := cid.Decode(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("failed to parse input: %w", err)
		}

		fsrepo, err := repo.NewFS(cctx.String("repo"))
		if err != nil {
			return err
		}

		lkrepo, err := fsrepo.Lock(repo.FullNode)
		if err != nil {
			return err
		}

		defer lkrepo.Close() //nolint:errcheck

		bs, err := lkrepo.Blockstore(ctx, repo.UniversalBlockstore)
		if err != nil {
			return fmt.Errorf("failed to open blockstore: %w", err)
		}

		defer func() {
			if c, ok := bs.(io.Closer); ok {
				if err := c.Close(); err != nil {
					log.Warnf("failed to close blockstore: %s", err)
				}
			}
		}()

		mds, err := lkrepo.Datastore(context.Background(), "/metadata")
		if err != nil {
			return err
		}

		cs := store.NewChainStore(bs, bs, mds, vm.Syscalls(ffiwrapper.ProofVerifier), nil)
		defer cs.Close() //nolint:errcheck

		cst := cbor.NewCborStore(bs)
		store := adt.WrapStore(ctx, cst)

		sm := stmgr.NewStateManager(cs)

		tree, err := state.LoadStateTree(cst, sroot)
		if err != nil {
			return err
		}

		minerInfo := cctx.Bool("miner-info")

		robustMap := make(map[address.Address]address.Address)
		if cctx.Bool("robust-addresses") {
			iact, err := tree.GetActor(_init.Address)
			if err != nil {
				return xerrors.Errorf("failed to load init actor: %w", err)
			}

			ist, err := _init.Load(store, iact)
			if err != nil {
				return xerrors.Errorf("failed to load init actor state: %w", err)
			}

			err = ist.ForEachActor(func(id abi.ActorID, addr address.Address) error {
				idAddr, err := address.NewIDAddress(uint64(id))
				if err != nil {
					return xerrors.Errorf("failed to write to addr map: %w", err)
				}

				robustMap[idAddr] = addr

				return nil
			})
			if err != nil {
				return xerrors.Errorf("failed to invert init address map: %w", err)
			}
		}

		var infos []accountInfo
		err = tree.ForEach(func(addr address.Address, act *types.Actor) error {

			ai := accountInfo{
				Address:     addr,
				Balance:     types.EPK(act.Balance),
				Type:        string(act.Code.Hash()[2:]),
				Power:       big.NewInt(0),
				LockedFunds: types.EPK(big.NewInt(0)),
				/* InitialPledge: types.EPK(big.NewInt(0)),
				PreCommits:    types.EPK(big.NewInt(0)), */
				VestingAmount: types.EPK(big.NewInt(0)),
			}

			if cctx.Bool("robust-addresses") {
				robust, found := robustMap[addr]
				if found {
					ai.Address = robust
				} else {
					id, err := address.IDFromAddress(addr)
					if err != nil {
						return xerrors.Errorf("failed to get ID address: %w", err)
					}

					// TODO: This is not the correctest way to determine whether a robust address should exist
					if id >= genesis.MinerStart {
						return xerrors.Errorf("address doesn't have a robust address: %s", addr)
					}
				}
			}

			if minerInfo && builtin.IsStorageMinerActor(act.Code) {
				pow, _, _, err := stmgr.GetPowerRaw(ctx, sm, sroot, addr, 0 /* meaningless */)
				if err != nil {
					return xerrors.Errorf("failed to get power: %w", err)
				}

				ai.Power = pow.RawBytePower

				st, err := miner.Load(store, act)
				if err != nil {
					return xerrors.Errorf("failed to read miner state: %w", err)
				}

				liveSectorCount, err := st.NumLiveSectors()
				if err != nil {
					return xerrors.Errorf("failed to compute live sector count: %w", err)
				}

				// funds, err := st.Funds()
				// if err != nil {
				// 	return xerrors.Errorf("failed to compute locked funds: %w", err)
				// }

				/* ai.InitialPledge = types.EPK(lockedFunds.InitialPledgeRequirement) */
				// ai.LockedFunds = types.EPK(funds.VestingFunds)
				/* ai.PreCommits = types.EPK(lockedFunds.PreCommitDeposits) */
				ai.Sectors = liveSectorCount

				minfo, err := st.Info()
				if err != nil {
					return xerrors.Errorf("failed to get miner info: %w", err)
				}

				ai.Worker = minfo.Worker
				ai.Owner = minfo.Owner
				ai.Coinbase = minfo.Coinbase
			}

			if builtin.IsMultisigActor(act.Code) {
				mst, err := multisig.Load(store, act)
				if err != nil {
					return err
				}

				ai.VestingStart, err = mst.StartEpoch()
				if err != nil {
					return err
				}

				ib, err := mst.InitialBalance()
				if err != nil {
					return err
				}

				ai.VestingAmount = types.EPK(ib)

				ai.VestingDuration, err = mst.UnlockDuration()
				if err != nil {
					return err
				}

			}

			infos = append(infos, ai)
			return nil
		})
		if err != nil {
			return xerrors.Errorf("failed to loop over actors: %w", err)
		}

		printAccountInfos(infos, minerInfo)

		return nil
	},
}

func printAccountInfos(infos []accountInfo, minerInfo bool) {
	if minerInfo {
		fmt.Printf("Address,Balance,Type,Sectors,Worker,Owner,Coinbase,Locked,VestingStart,VestingDuration,VestingAmount\n")
		for _, acc := range infos {
			fmt.Printf("%s,%s,%s,%d,%s,%s,%s,%s,%d,%d,%s\n", acc.Address, acc.Balance.Unitless(), acc.Type, acc.Sectors, acc.Worker, acc.Owner, acc.Coinbase /* acc.InitialPledge.Unitless(), */, acc.LockedFunds.Unitless() /* acc.PreCommits.Unitless(),  */, acc.VestingStart, acc.VestingDuration, acc.VestingAmount.Unitless())
		}
	} else {
		fmt.Printf("Address,Balance,Type\n")
		for _, acc := range infos {
			fmt.Printf("%s,%s,%s\n", acc.Address, acc.Balance.Unitless(), acc.Type)
		}
	}

}

/* var chainPledgeCmd = &cli.Command{
	Name:        "stateroot-pledge",
	Description: "Calculate sector pledge numbers",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "repo",
			Value: "~/.epik",
		},
	},
	ArgsUsage: "[stateroot epoch]",
	Action: func(cctx *cli.Context) error {
		logging.SetLogLevel("badger", "ERROR")
		ctx := context.TODO()

		if !cctx.Args().Present() {
			return fmt.Errorf("must pass state root")
		}

		sroot, err := cid.Decode(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("failed to parse input: %w", err)
		}

		epoch, err := strconv.ParseInt(cctx.Args().Get(1), 10, 64)
		if err != nil {
			return xerrors.Errorf("parsing epoch arg: %w", err)
		}

		fsrepo, err := repo.NewFS(cctx.String("repo"))
		if err != nil {
			return err
		}

		lkrepo, err := fsrepo.Lock(repo.FullNode)
		if err != nil {
			return err
		}

		defer lkrepo.Close() //nolint:errcheck

		bs, err := lkrepo.Blockstore(ctx, repo.UniversalBlockstore)
		if err != nil {
			return xerrors.Errorf("failed to open blockstore: %w", err)
		}

		defer func() {
			if c, ok := bs.(io.Closer); ok {
				if err := c.Close(); err != nil {
					log.Warnf("failed to close blockstore: %s", err)
				}
			}
		}()

		mds, err := lkrepo.Datastore(context.Background(), "/metadata")
		if err != nil {
			return err
		}

		cs := store.NewChainStore(bs, bs, mds, vm.Syscalls(ffiwrapper.ProofVerifier), nil)
		defer cs.Close() //nolint:errcheck

		cst := cbor.NewCborStore(bs)
		store := adt.WrapStore(ctx, cst)

		sm := stmgr.NewStateManager(cs)

		state, err := state.LoadStateTree(cst, sroot)
		if err != nil {
			return err
		}

		var (
			powerSmoothed    builtin.FilterEstimate
			pledgeCollateral abi.TokenAmount
		)
		if act, err := state.GetActor(power.Address); err != nil {
			return xerrors.Errorf("loading miner actor: %w", err)
		} else if s, err := power.Load(store, act); err != nil {
			return xerrors.Errorf("loading power actor state: %w", err)
		} else if p, err := s.TotalPowerSmoothed(); err != nil {
			return xerrors.Errorf("failed to determine total power: %w", err)
		} else if c, err := s.TotalLocked(); err != nil {
			return xerrors.Errorf("failed to determine pledge collateral: %w", err)
		} else {
			powerSmoothed = p
			pledgeCollateral = c
		}

		circ, err := sm.GetVMCirculatingSupplyDetailed(ctx, abi.ChainEpoch(epoch), state)
		if err != nil {
			return err
		}

		fmt.Println("(real) circulating supply: ", types.EPK(circ.EpkCirculating))
		if circ.EpkCirculating.LessThan(big.Zero()) {
			circ.EpkCirculating = big.Zero()
		}

		rewardActor, err := state.GetActor(reward.Address)
		if err != nil {
			return xerrors.Errorf("loading miner actor: %w", err)
		}

		rewardState, err := reward.Load(store, rewardActor)
		if err != nil {
			return xerrors.Errorf("loading reward actor state: %w", err)
		}

		fmt.Println("EpkVested", types.EPK(circ.EpkVested))
		fmt.Println("EpkMined", types.EPK(circ.EpkMined))
		fmt.Println("EpkBurnt", types.EPK(circ.EpkBurnt))
		fmt.Println("EpkLocked", types.EPK(circ.EpkLocked))
		fmt.Println("EpkCirculating", types.EPK(circ.EpkCirculating))

		for _, sectorWeight := range []abi.StoragePower{
			types.NewInt(32 << 30),
			types.NewInt(64 << 30),
			types.NewInt(32 << 30 * 10),
			types.NewInt(64 << 30 * 10),
		} {
			initialPledge, err := rewardState.InitialPledgeForPower(
				sectorWeight,
				pledgeCollateral,
				&powerSmoothed,
				circ.EpkCirculating,
			)
			if err != nil {
				return xerrors.Errorf("calculating initial pledge: %w", err)
			}

			fmt.Println("IP ", units.HumanSize(float64(sectorWeight.Uint64())), types.EPK(initialPledge))
		}

		return nil
	},
} */

const dateFmt = "1/02/06"

func parseCsv(inp string) ([]time.Time, []address.Address, error) {
	fi, err := os.Open(inp)
	if err != nil {
		return nil, nil, err
	}

	r := csv.NewReader(fi)
	recs, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}

	var addrs []address.Address
	for _, rec := range recs[1:] {
		a, err := address.NewFromString(rec[0])
		if err != nil {
			return nil, nil, err
		}
		addrs = append(addrs, a)
	}

	var dates []time.Time
	for _, d := range recs[0][1:] {
		if len(d) == 0 {
			continue
		}
		p := strings.Split(d, " ")
		t, err := time.Parse(dateFmt, p[len(p)-1])
		if err != nil {
			return nil, nil, err
		}

		dates = append(dates, t)
	}

	return dates, addrs, nil
}

func heightForDate(d time.Time, ts *types.TipSet) abi.ChainEpoch {
	secs := d.Unix()
	gents := ts.Blocks()[0].Timestamp
	gents -= uint64(30 * ts.Height())
	return abi.ChainEpoch((secs - int64(gents)) / 30)
}

var fillBalancesCmd = &cli.Command{
	Name:        "fill-balances",
	Description: "fill out balances for addresses on dates in given spreadsheet",
	Flags:       []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}

		defer closer()
		ctx := lcli.ReqContext(cctx)

		dates, addrs, err := parseCsv(cctx.Args().First())
		if err != nil {
			return err
		}

		ts, err := api.ChainHead(ctx)
		if err != nil {
			return err
		}

		var tipsets []*types.TipSet
		for _, d := range dates {
			h := heightForDate(d, ts)
			hts, err := api.ChainGetTipSetByHeight(ctx, h, ts.Key())
			if err != nil {
				return err
			}
			tipsets = append(tipsets, hts)
		}

		var balances [][]abi.TokenAmount
		for _, a := range addrs {
			var b []abi.TokenAmount
			for _, hts := range tipsets {
				act, err := api.StateGetActor(ctx, a, hts.Key())
				if err != nil {
					if !strings.Contains(err.Error(), "actor not found") {
						return fmt.Errorf("error for %s at %s: %w", a, hts.Key(), err)
					}
					b = append(b, types.NewInt(0))
					continue
				}
				b = append(b, act.Balance)
			}
			balances = append(balances, b)
		}

		var datestrs []string
		for _, d := range dates {
			datestrs = append(datestrs, "Balance at "+d.Format(dateFmt))
		}

		w := csv.NewWriter(os.Stdout)
		w.Write(append([]string{"Wallet Address"}, datestrs...)) // nolint:errcheck
		for i := 0; i < len(addrs); i++ {
			row := []string{addrs[i].String()}
			for _, b := range balances[i] {
				row = append(row, types.EPK(b).String())
			}
			w.Write(row) // nolint:errcheck
		}
		w.Flush()
		return nil
	},
}
