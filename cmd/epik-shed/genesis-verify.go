package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin"

	"github.com/fatih/color"
	"github.com/ipfs/go-datastore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/EpiK-Protocol/go-epik/blockstore"
	"github.com/EpiK-Protocol/go-epik/build"
	"github.com/EpiK-Protocol/go-epik/chain/actors/adt"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/account"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/miner"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/multisig"
	"github.com/EpiK-Protocol/go-epik/chain/state"
	"github.com/EpiK-Protocol/go-epik/chain/stmgr"
	"github.com/EpiK-Protocol/go-epik/chain/store"
	"github.com/EpiK-Protocol/go-epik/chain/types"
)

type addrInfo struct {
	Key     address.Address
	Balance types.EPK
}

type msigInfo struct {
	Signers   []address.Address
	Balance   types.EPK
	Threshold uint64
}

type minerInfo struct {
}

var genesisVerifyCmd = &cli.Command{
	Name:        "verify-genesis",
	Description: "verify some basic attributes of a genesis car file",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("must pass genesis car file")
		}
		bs := blockstore.FromDatastore(datastore.NewMapDatastore())

		cs := store.NewChainStore(bs, bs, datastore.NewMapDatastore(), nil, nil)
		defer cs.Close() //nolint:errcheck

		cf := cctx.Args().Get(0)
		f, err := os.Open(cf)
		if err != nil {
			return xerrors.Errorf("opening the car file: %w", err)
		}

		ts, err := cs.Import(f)
		if err != nil {
			return err
		}

		sm := stmgr.NewStateManager(cs)

		total, err := stmgr.CheckTotalEPK(context.TODO(), sm, ts)
		if err != nil {
			return err
		}

		fmt.Println("Genesis: ", ts.Key())
		expEPK := big.Mul(big.NewInt(int64(build.EpkBase)), big.NewInt(int64(build.EpkPrecision)))
		fmt.Printf("Total EPK: %s", types.EPK(total))
		if !expEPK.Equals(total) {
			color.Red("  INCORRECT!")
		}
		fmt.Println()

		cst := cbor.NewCborStore(bs)

		stree, err := state.LoadStateTree(cst, ts.ParentState())
		if err != nil {
			return err
		}

		var accAddrs, msigAddrs []address.Address
		kaccounts := make(map[address.Address]addrInfo)
		kmultisigs := make(map[address.Address]msigInfo)
		kminers := make(map[address.Address]minerInfo)

		ctx := context.TODO()
		store := adt.WrapStore(ctx, cst)

		if err := stree.ForEach(func(addr address.Address, act *types.Actor) error {
			switch {
			case builtin.IsStorageMinerActor(act.Code):
				_, err := miner.Load(store, act)
				if err != nil {
					return xerrors.Errorf("miner actor: %w", err)
				}
				// TODO: actually verify something here?
				kminers[addr] = minerInfo{}
			case builtin.IsMultisigActor(act.Code):
				st, err := multisig.Load(store, act)
				if err != nil {
					return xerrors.Errorf("multisig actor: %w", err)
				}

				signers, err := st.Signers()
				if err != nil {
					return xerrors.Errorf("multisig actor: %w", err)
				}
				threshold, err := st.Threshold()
				if err != nil {
					return xerrors.Errorf("multisig actor: %w", err)
				}

				kmultisigs[addr] = msigInfo{
					Balance:   types.EPK(act.Balance),
					Signers:   signers,
					Threshold: threshold,
				}
				msigAddrs = append(msigAddrs, addr)
			case builtin.IsAccountActor(act.Code):
				st, err := account.Load(store, act)
				if err != nil {
					// TODO: magik6k: this _used_ to log instead of failing, why?
					return xerrors.Errorf("account actor %s: %w", addr, err)
				}
				pkaddr, err := st.PubkeyAddress()
				if err != nil {
					return xerrors.Errorf("failed to get actor pk address %s: %w", addr, err)
				}
				kaccounts[addr] = addrInfo{
					Key:     pkaddr,
					Balance: types.EPK(act.Balance.Copy()),
				}
				accAddrs = append(accAddrs, addr)
			}
			return nil
		}); err != nil {
			return err
		}

		sort.Slice(accAddrs, func(i, j int) bool {
			return accAddrs[i].String() < accAddrs[j].String()
		})

		sort.Slice(msigAddrs, func(i, j int) bool {
			return msigAddrs[i].String() < msigAddrs[j].String()
		})

		fmt.Println("Account Actors:")
		for _, acc := range accAddrs {
			a := kaccounts[acc]
			fmt.Printf("%s\t%s\t%s\n", acc, a.Key, a.Balance)
		}

		fmt.Println("Multisig Actors:")
		for _, acc := range msigAddrs {
			m := kmultisigs[acc]
			fmt.Printf("%s\t%s\t%d\t[", acc, m.Balance, m.Threshold)
			for i, s := range m.Signers {
				fmt.Print(s)
				if i != len(m.Signers)-1 {
					fmt.Print(",")
				}
			}
			fmt.Printf("]\n")
		}
		return nil
	},
}
