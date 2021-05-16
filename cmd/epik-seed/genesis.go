package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/EpiK-Protocol/go-epik/blockstore"
	"github.com/EpiK-Protocol/go-epik/chain/vm"
	"github.com/EpiK-Protocol/go-epik/extern/sector-storage/ffiwrapper"
	"github.com/EpiK-Protocol/go-epik/journal"
	"github.com/EpiK-Protocol/go-epik/node/modules/testing"
	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"

	"github.com/EpiK-Protocol/go-epik/build"
	"github.com/EpiK-Protocol/go-epik/chain/gen"
	genesis2 "github.com/EpiK-Protocol/go-epik/chain/gen/genesis"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/genesis"
)

var genesisCmd = &cli.Command{
	Name:        "genesis",
	Description: "manipulate epik genesis template",
	Subcommands: []*cli.Command{
		genesisNewCmd,
		genesisAddMinerCmd,
		genesisAddMsigsCmd,
		genesisCarCmd,
	},
}

var genesisNewCmd = &cli.Command{
	Name:        "new",
	Description: "create new genesis template",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "network-name",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.New("seed genesis new [genesis.json]")
		}
		out := genesis.Template{
			Accounts:    []genesis.Actor{},
			Miners:      []genesis.Miner{},
			NetworkName: cctx.String("network-name"),

			TeamAccountActor:        gen.DefaultTeamAccountActor,
			FoundationAccountActor:  gen.DefaultFoundationAccountActor,
			InvestorAccountActor:    gen.DefaultInvestorAccountActor,
			DefaultGovernorActor:    gen.DefaultGovernorActor,
			DefaultExpertActor:      gen.DefaultExpertActor,
			DefaultKgFundPayeeActor: gen.DefaultKgFundPayeeActor,
		}
		if out.NetworkName == "" {
			out.NetworkName = "localnet-" + uuid.New().String()
		}

		genb, err := json.MarshalIndent(&out, "", "  ")
		if err != nil {
			return err
		}

		genf, err := homedir.Expand(cctx.Args().First())
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(genf, genb, 0644); err != nil {
			return err
		}

		return nil
	},
}

var genesisAddMinerCmd = &cli.Command{
	Name:        "add-miner",
	Description: "add genesis miner",
	Flags:       []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return xerrors.New("seed genesis add-miner [genesis.json] [preseal.json]")
		}

		genf, err := homedir.Expand(cctx.Args().First())
		if err != nil {
			return err
		}

		var template genesis.Template
		genb, err := ioutil.ReadFile(genf)
		if err != nil {
			return xerrors.Errorf("read genesis template: %w", err)
		}

		if err := json.Unmarshal(genb, &template); err != nil {
			return xerrors.Errorf("unmarshal genesis template: %w", err)
		}

		minf, err := homedir.Expand(cctx.Args().Get(1))
		if err != nil {
			return xerrors.Errorf("expand preseal file path: %w", err)
		}
		miners := map[string]genesis.Miner{}
		minb, err := ioutil.ReadFile(minf)
		if err != nil {
			return xerrors.Errorf("read preseal file: %w", err)
		}
		if err := json.Unmarshal(minb, &miners); err != nil {
			return xerrors.Errorf("unmarshal miner info: %w", err)
		}

		for mn, miner := range miners {
			log.Infof("Adding miner %s to genesis template", mn)
			{
				id := uint64(genesis2.MinerStart) + uint64(len(template.Miners)+1) // plus 1 expert
				maddr, err := address.NewFromString(mn)
				if err != nil {
					return xerrors.Errorf("parsing miner address: %w", err)
				}
				mid, err := address.IDFromAddress(maddr)
				if err != nil {
					return xerrors.Errorf("getting miner id from address: %w", err)
				}
				if mid != id {
					return xerrors.Errorf("tried to set miner t0%d as t0%d", mid, id)
				}
			}

			template.Miners = append(template.Miners, miner)
			log.Infof("Giving %s some initial balance", miner.Owner)
			template.Accounts = append(template.Accounts, genesis.Actor{
				Type:    genesis.TAccount,
				Balance: big.Mul(big.NewInt(2002), big.NewInt(int64(build.EpkPrecision))),
				Meta:    (&genesis.AccountMeta{Owner: miner.Owner}).ActorMeta(),
			})
		}

		genb, err = json.MarshalIndent(&template, "", "  ")
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(genf, genb, 0644); err != nil {
			return err
		}

		return nil
	},
}

type GenAccountEntry struct {
	Version          int
	ID               string
	Amount           types.EPK
	VestingMonths    int
	CustodianID      int
	M                int
	N                int
	Addresses        []address.Address
	Type             string
	Sig1             string
	Sig2             string
	InitialVestedNum int
	InitialVestedDen int
}

var genesisAddMsigsCmd = &cli.Command{
	Name: "add-msigs",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 2 {
			return fmt.Errorf("must specify template file and csv file with accounts")
		}

		genf, err := homedir.Expand(cctx.Args().First())
		if err != nil {
			return err
		}

		csvf, err := homedir.Expand(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		var template genesis.Template
		b, err := ioutil.ReadFile(genf)
		if err != nil {
			return xerrors.Errorf("read genesis template: %w", err)
		}

		if err := json.Unmarshal(b, &template); err != nil {
			return xerrors.Errorf("unmarshal genesis template: %w", err)
		}

		entries, err := parseMultisigCsv(csvf)
		if err != nil {
			return xerrors.Errorf("parsing multisig csv file: %w", err)
		}

		for i, e := range entries {
			if len(e.Addresses) != e.N {
				return fmt.Errorf("entry %d had mismatch between 'N' and number of addresses", i)
			}

			msig := &genesis.MultisigMeta{
				Signers:         e.Addresses,
				Threshold:       e.M,
				VestingDuration: monthsToBlocks(e.VestingMonths),
				VestingStart:    0,
			}
			if e.InitialVestedNum > 0 && e.InitialVestedDen > 0 {
				msig.InitialVestedTarget = &builtin.BigFrac{
					Numerator:   big.NewInt(int64(e.InitialVestedNum)),
					Denominator: big.NewInt(int64(e.InitialVestedDen)),
				}
			}

			act := genesis.Actor{
				Type:    genesis.TMultisig,
				Balance: abi.TokenAmount(e.Amount),
				Meta:    msig.ActorMeta(),
			}

			template.Accounts = append(template.Accounts, act)

		}

		b, err = json.MarshalIndent(&template, "", "  ")
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(genf, b, 0644); err != nil {
			return err
		}
		return nil
	},
}

func monthsToBlocks(nmonths int) int {
	days := uint64((365 * nmonths) / 12)
	return int(days * 24 * 60 * 60 / build.BlockDelaySecs)
}

func parseMultisigCsv(csvf string) ([]GenAccountEntry, error) {
	fileReader, err := os.Open(csvf)
	if err != nil {
		return nil, xerrors.Errorf("read multisig csv: %w", err)
	}
	defer fileReader.Close() //nolint:errcheck
	r := csv.NewReader(fileReader)
	records, err := r.ReadAll()
	if err != nil {
		return nil, xerrors.Errorf("read multisig csv: %w", err)
	}
	var entries []GenAccountEntry
	for i, e := range records[1:] {
		var addrs []address.Address
		addrStrs := strings.Split(strings.TrimSpace(e[7]), ":")
		for j, a := range addrStrs {
			addr, err := address.NewFromString(a)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse address %d in row %d (%q): %w", j, i, a, err)
			}
			addrs = append(addrs, addr)
		}

		initialVestedStr := strings.TrimSpace(e[11])
		var initialNum, initialDen int
		if len(initialVestedStr) > 0 {
			target := strings.Split(initialVestedStr, "/")
			if len(target) != 2 {
				return nil, xerrors.Errorf("failed to parse initial vested")
			}
			initialNum, err = strconv.Atoi(strings.TrimSpace(target[0]))
			if err != nil {
				return nil, xerrors.Errorf("Numerator be integer: %w", err)
			}
			initialDen, err = strconv.Atoi(strings.TrimSpace(target[1]))
			if err != nil {
				return nil, xerrors.Errorf("Denominator be integer: %w", err)
			}
			if initialNum < 0 || initialDen <= 0 || initialNum > initialDen {
				return nil, xerrors.Errorf("illegal numerator %d or denominator %d", initialNum, initialDen)
			}
		}

		balance, err := types.ParseEPK(strings.TrimSpace(e[2]))
		if err != nil {
			return nil, xerrors.Errorf("failed to parse account balance: %w", err)
		}

		vesting, err := strconv.Atoi(strings.TrimSpace(e[3]))
		if err != nil {
			return nil, xerrors.Errorf("failed to parse vesting duration for record %d: %w", i, err)
		}

		custodianID, err := strconv.Atoi(strings.TrimSpace(e[4]))
		if err != nil {
			return nil, xerrors.Errorf("failed to parse custodianID in record %d: %w", i, err)
		}
		threshold, err := strconv.Atoi(strings.TrimSpace(e[5]))
		if err != nil {
			return nil, xerrors.Errorf("failed to parse multisigM in record %d: %w", i, err)
		}
		num, err := strconv.Atoi(strings.TrimSpace(e[6]))
		if err != nil {
			return nil, xerrors.Errorf("Number of addresses be integer: %w", err)
		}
		if e[0] != "1" {
			return nil, xerrors.Errorf("record version must be 1")
		}
		entries = append(entries, GenAccountEntry{
			Version:          1,
			ID:               e[1],
			Amount:           balance,     // e[2]
			VestingMonths:    vesting,     // e[3]
			CustodianID:      custodianID, // e[4]
			M:                threshold,   // e[5]
			N:                num,         // e[6]
			Addresses:        addrs,       // e[7]
			Type:             e[8],
			Sig1:             e[9],
			Sig2:             e[10],
			InitialVestedNum: initialNum,
			InitialVestedDen: initialDen,
		})
	}

	return entries, nil
}

var genesisCarCmd = &cli.Command{
	Name:        "car",
	Description: "write genesis car file",
	ArgsUsage:   "genesis template `FILE`",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "out",
			Aliases: []string{"o"},
			Value:   "genesis.car",
			Usage:   "write output to `FILE`",
		},
	},
	Action: func(c *cli.Context) error {
		if c.Args().Len() != 1 {
			return xerrors.Errorf("Please specify a genesis template. (i.e, the one created with `genesis new`)")
		}
		ofile := c.String("out")
		jrnl := journal.NilJournal()
		bstor := blockstore.NewMemorySync()
		sbldr := vm.Syscalls(ffiwrapper.ProofVerifier)
		_, err := testing.MakeGenesis(ofile, c.Args().First())(bstor, sbldr, jrnl)()
		return err
	},
}
