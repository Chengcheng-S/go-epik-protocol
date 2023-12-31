package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	tm "github.com/buger/goterm"
	"github.com/chzyer/readline"
	"github.com/docker/go-units"
	"github.com/fatih/color"
	datatransfer "github.com/filecoin-project/go-data-transfer"
	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-cidutil/cidenc"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/multiformats/go-multibase"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/filecoin-project/go-multistore"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/EpiK-Protocol/go-epik/api"
	lapi "github.com/EpiK-Protocol/go-epik/api"
	"github.com/EpiK-Protocol/go-epik/chain/actors"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/market"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/miner"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/lib/tablewriter"

	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
)

var CidBaseFlag = cli.StringFlag{
	Name:        "cid-base",
	Hidden:      true,
	Value:       "base32",
	Usage:       "Multibase encoding used for version 1 CIDs in output.",
	DefaultText: "base32",
}

// GetCidEncoder returns an encoder using the `cid-base` flag if provided, or
// the default (Base32) encoder if not.
func GetCidEncoder(cctx *cli.Context) (cidenc.Encoder, error) {
	val := cctx.String("cid-base")

	e := cidenc.Encoder{Base: multibase.MustNewEncoder(multibase.Base32)}

	if val != "" {
		var err error
		e.Base, err = multibase.EncoderByName(val)
		if err != nil {
			return e, err
		}
	}

	return e, nil
}

var clientCmd = &cli.Command{
	Name:  "client",
	Usage: "Make deals, store data, retrieve data",
	Subcommands: []*cli.Command{
		WithCategory("storage", clientDealCmd),
		WithCategory("storage", clientQueryAskCmd),
		WithCategory("storage", clientListDeals),
		WithCategory("storage", clientGetDealCmd),
		WithCategory("storage", clientListAsksCmd),
		WithCategory("storage", clientDealStatsCmd),
		WithCategory("storage", miningPledgeCmd),
		WithCategory("data", clientImportCmd),
		WithCategory("data", clientExportCmd),
		WithCategory("data", clientDropCmd),
		WithCategory("data", clientLocalCmd),
		WithCategory("data", clientStat),
		WithCategory("retrieval", clientFindCmd),
		WithCategory("retrieval", clientRetrieveCmd),
		WithCategory("retrieval", clientRetrieveDealCmd),
		WithCategory("retrieval", clientRetrieveListCmd),
		WithCategory("retrieval", clientRetrievePledgeCmd),
		WithCategory("retrieval", clientRetrieveBindCmd),
		WithCategory("retrieval", clientRetrieveMinerCmd),
		WithCategory("retrieval", clientRetrieveInfoCmd),
		WithCategory("retrieval", clientRetrievePledgeStateCmd),
		WithCategory("retrieval", clientRetrieveApplyForWithdrawCmd),
		WithCategory("retrieval", clientRetrieveWithdrawCmd),
		WithCategory("util", clientCommPCmd),
		WithCategory("util", clientCarGenCmd),
		// WithCategory("util", clientBalancesCmd),
		WithCategory("util", clientListTransfers),
		WithCategory("util", clientRestartTransfer),
		WithCategory("util", clientCancelTransfer),
		WithCategory("util", clientCancelAllTransfer),
	},
}

var clientImportCmd = &cli.Command{
	Name:      "import",
	Usage:     "Import data",
	ArgsUsage: "[inputPath]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "car",
			Usage: "import from a car file instead of a regular file",
		},
		&cli.BoolFlag{
			Name:  "local",
			Usage: "import the file to local and not start storage deal",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "expert",
			Usage: "specify the data submit expert, '--from' will be ignored if set",
		},
		&cli.StringFlag{
			Name:  "miner",
			Usage: "specify the data deal miner",
		},
		&cli.StringFlag{
			Name:  "from",
			Usage: "specify address to import file as, replaced by expert owner if '--expert' set",
		},
		&CidBaseFlag,
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if cctx.NArg() != 1 {
			return xerrors.New("expected input path as the only arg")
		}

		var absPath string
		path := cctx.Args().First()
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			absPath = path
		} else {
			absPath, err = filepath.Abs(path)
			if err != nil {
				return err
			}
		}

		var res *lapi.ImportRes
		if cctx.Bool("local") {
			res, err = api.ClientImport(ctx, lapi.FileRef{
				Path:  absPath,
				IsCAR: cctx.Bool("car"),
			})
			if err != nil {
				return err
			}
		} else {
			// from
			var from address.Address
			if v := cctx.String("from"); v != "" {
				from, err = address.NewFromString(v)
				if err != nil {
					return xerrors.Errorf("failed to parse 'from' address: %w", err)
				}
			} else {
				from, err = api.WalletDefaultAddress(ctx)
				if err != nil {
					return err
				}
			}

			// expert
			var expert address.Address
			if v := cctx.String("expert"); v != "" {
				expert, err = address.NewFromString(v)
				if err != nil {
					return err
				}
			}

			// miner
			var miner address.Address
			if miner, err = address.NewFromString(cctx.String("miner")); err != nil {

				if err != nil {
					return xerrors.Errorf("failed to parse 'miner' address to start deal: %w", err)
				}
			}

			res, err = api.ClientImportAndDeal(ctx, &lapi.ImportAndDealParams{
				Ref: lapi.FileRef{
					Path:  absPath,
					IsCAR: cctx.Bool("car"),
				},
				Miner:  miner,
				From:   from,
				Expert: expert,
			})
			if err != nil {
				return err
			}
		}

		ds, err := api.ClientDealPieceCID(ctx, res.Root)
		if err != nil {
			return err
		}

		encoder, err := GetCidEncoder(cctx)
		if err != nil {
			return err
		}

		fmt.Printf("Import ID: %d\n", res.ImportID)
		fmt.Printf("Root: %s\n", encoder.Encode(res.Root))
		fmt.Printf("Piece CID: %s\n", encoder.Encode(ds.PieceCID))
		fmt.Printf("Piece Size: %d\n", ds.PieceSize)
		fmt.Printf("Payload Size: %d\n", ds.PayloadSize)

		return nil
	},
}

var clientExportCmd = &cli.Command{
	Name:      "export",
	Usage:     "export import file",
	ArgsUsage: "[rootID] [output path]",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "car",
			Usage: "export to a car file instead of a regular file",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return fmt.Errorf("usage: export [rootID] [output path]")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		rootID, err := cid.Parse(cctx.Args().Get(0))
		if err != nil {
			return err
		}

		path := cctx.Args().Get(1)

		isCar := cctx.Bool("car")

		if err := api.ClientExport(ctx, lapi.ExportRef{Root: rootID}, lapi.FileRef{Path: path, IsCAR: isCar}); err != nil {
			return xerrors.Errorf("export %d: %w", rootID, err)
		}

		return nil
	},
}

var clientDropCmd = &cli.Command{
	Name:      "drop",
	Usage:     "Remove import",
	ArgsUsage: "[import ID...]",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.Errorf("no imports specified")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var ids []multistore.StoreID
		for i, s := range cctx.Args().Slice() {
			id, err := strconv.ParseInt(s, 10, 0)
			if err != nil {
				return xerrors.Errorf("parsing %d-th import ID: %w", i, err)
			}

			ids = append(ids, multistore.StoreID(id))
		}

		for _, id := range ids {
			if err := api.ClientRemoveImport(ctx, id); err != nil {
				return xerrors.Errorf("removing import %d: %w", id, err)
			}
		}

		return nil
	},
}

var clientCommPCmd = &cli.Command{
	Name:      "commP",
	Usage:     "Calculate the piece-cid (commP) of a CAR file",
	ArgsUsage: "[inputFile]",
	Flags: []cli.Flag{
		&CidBaseFlag,
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if cctx.Args().Len() != 1 {
			return fmt.Errorf("usage: commP <inputPath>")
		}

		ret, err := api.ClientCalcCommP(ctx, cctx.Args().Get(0))
		if err != nil {
			return err
		}

		encoder, err := GetCidEncoder(cctx)
		if err != nil {
			return err
		}

		fmt.Println("CID: ", encoder.Encode(ret.Root))
		fmt.Println("Piece size: ", types.SizeStr(types.NewInt(uint64(ret.Size))))
		return nil
	},
}

var clientCarGenCmd = &cli.Command{
	Name:      "generate-car",
	Usage:     "Generate a car file from input",
	ArgsUsage: "[inputPath outputPath]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if cctx.Args().Len() != 2 {
			return fmt.Errorf("usage: generate-car <inputPath> <outputPath>")
		}

		ref := lapi.FileRef{
			Path:  cctx.Args().First(),
			IsCAR: false,
		}

		op := cctx.Args().Get(1)

		if err = api.ClientGenCar(ctx, ref, op); err != nil {
			return err
		}
		return nil
	},
}

var clientLocalCmd = &cli.Command{
	Name:  "local",
	Usage: "List locally imported data",
	Flags: []cli.Flag{
		&CidBaseFlag,
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		list, err := api.ClientListImports(ctx)
		if err != nil {
			return err
		}

		encoder, err := GetCidEncoder(cctx)
		if err != nil {
			return err
		}

		sort.Slice(list, func(i, j int) bool {
			return list[i].Key < list[j].Key
		})

		for _, v := range list {
			cidStr := "<nil>"
			if v.Root != nil {
				cidStr = encoder.Encode(*v.Root)
			}

			fmt.Printf("%d: %s @%s (%s)\n", v.Key, cidStr, v.FilePath, v.Source)
			if v.Err != "" {
				fmt.Printf("\terror: %s\n", v.Err)
			}
		}
		return nil
	},
}

var clientDealCmd = &cli.Command{
	Name:      "deal",
	Usage:     "Initialize storage deal with a miner",
	ArgsUsage: "[dataCid miner]",
	// ArgsUsage: "[dataCid miner price duration]",
	Flags: []cli.Flag{
		// &cli.StringFlag{
		// 	Name:  "manual-piece-cid",
		// 	Usage: "manually specify piece commitment for data (dataCid must be to a car file)",
		// },
		// &cli.Int64Flag{
		// 	Name:  "manual-piece-size",
		// 	Usage: "if manually specifying piece cid, used to specify size (dataCid must be to a car file)",
		// },
		&cli.StringFlag{
			Name:  "from",
			Usage: "specify address to fund the deal with",
		},
		&cli.Int64Flag{
			Name:  "start-epoch",
			Usage: "specify the epoch that the deal should start at",
			Value: -1,
		},
		// &cli.BoolFlag{
		// 	Name:  "fast-retrieval",
		// 	Usage: "indicates that data should be available for fast retrieval",
		// 	Value: true,
		// },
		/* &cli.BoolFlag{
			Name:        "verified-deal",
			Usage:       "indicate that the deal counts towards verified client total",
			DefaultText: "true if client is verified, false otherwise",
		},
		&cli.StringFlag{
			Name:  "provider-collateral",
			Usage: "specify the requested provider collateral the miner should put up",
		}, */
		&CidBaseFlag,
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return interactiveDeal(cctx)
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)
		afmt := NewAppFmt(cctx.App)

		if cctx.NArg() != 2 {
			return xerrors.New("expected 2 args: dataCid, miner")
		}

		// [data, miner, price, dur]

		data, err := cid.Parse(cctx.Args().Get(0))
		if err != nil {
			return err
		}

		miner, err := address.NewFromString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		/* price, err := types.ParseEPK(cctx.Args().Get(2))
		if err != nil {
			return err
		}

		dur, err := strconv.ParseInt(cctx.Args().Get(3), 10, 32)
		if err != nil {
			return err
		}

		var provCol big.Int
		if pcs := cctx.String("provider-collateral"); pcs != "" {
			pc, err := big.FromString(pcs)
			if err != nil {
				return fmt.Errorf("failed to parse provider-collateral: %w", err)
			}
			provCol = pc
		}

		if abi.ChainEpoch(dur) < build.MinDealDuration {
			return xerrors.Errorf("minimum deal duration is %d blocks", build.MinDealDuration)
		} */

		var a address.Address
		if from := cctx.String("from"); from != "" {
			faddr, err := address.NewFromString(from)
			if err != nil {
				return xerrors.Errorf("failed to parse 'from' address: %w", err)
			}
			a = faddr
		} else {
			def, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}
			a = def
		}

		ref := &storagemarket.DataRef{
			TransferType: storagemarket.TTGraphsync,
			Root:         data,
		}

		// if mpc := cctx.String("manual-piece-cid"); mpc != "" {
		// 	c, err := cid.Parse(mpc)
		// 	if err != nil {
		// 		return xerrors.Errorf("failed to parse provided manual piece cid: %w", err)
		// 	}

		// 	ref.PieceCid = &c

		// 	psize := cctx.Int64("manual-piece-size")
		// 	if psize == 0 {
		// 		return xerrors.Errorf("must specify piece size when manually setting cid")
		// 	}

		// 	ref.PieceSize = abi.UnpaddedPieceSize(psize)

		// 	ref.TransferType = storagemarket.TTManual
		// }

		/* // Check if the address is a verified client
		dcap, err := api.StateVerifiedClientStatus(ctx, a, types.EmptyTSK)
		if err != nil {
			return err
		}

		isVerified := dcap != nil

		// If the user has explicitly set the --verified-deal flag
		if cctx.IsSet("verified-deal") {
			// If --verified-deal is true, but the address is not a verified
			// client, return an error
			verifiedDealParam := cctx.Bool("verified-deal")
			if verifiedDealParam && !isVerified {
				return xerrors.Errorf("address %s does not have verified client status", a)
			}

			// Override the default
			isVerified = verifiedDealParam
		} */

		proposal, err := api.ClientStartDeal(ctx, &lapi.StartDealParams{
			Data:   ref,
			Wallet: a,
			Miner:  miner,
			/* EpochPrice:         types.BigInt(price),
			MinBlocksDuration:  uint64(dur), */
			DealStartEpoch: abi.ChainEpoch(cctx.Int64("start-epoch")),
			FastRetrieval:  true,
			/* VerifiedDeal:       isVerified,
			ProviderCollateral: provCol, */
		})
		if err != nil {
			return err
		}

		encoder, err := GetCidEncoder(cctx)
		if err != nil {
			return err
		}

		afmt.Println(encoder.Encode(*proposal))

		return nil
	},
}

func interactiveDeal(cctx *cli.Context) error {
	api, closer, err := GetFullNodeAPI(cctx)
	if err != nil {
		return err
	}
	defer closer()
	ctx := ReqContext(cctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	afmt := NewAppFmt(cctx.App)

	state := "import"
	// gib := types.NewInt(1 << 30)

	var data cid.Cid
	// var days int
	var maddrs []address.Address
	var ask []storagemarket.StorageAsk
	// var epochPrices []big.Int
	// var dur time.Duration
	// var epochs abi.ChainEpoch
	// var verified bool
	var ds lapi.DataCIDSize

	// find
	var candidateAsks []*storagemarket.StorageAsk
	// var budget types.EPK
	var dealCount int64

	var a address.Address
	if from := cctx.String("from"); from != "" {
		faddr, err := address.NewFromString(from)
		if err != nil {
			return xerrors.Errorf("failed to parse 'from' address: %w", err)
		}
		a = faddr
	} else {
		def, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			return err
		}
		a = def
	}

	fromBal, err := api.WalletBalance(ctx, a)
	if err != nil {
		return xerrors.Errorf("checking from address balance: %w", err)
	}

	printErr := func(err error) {
		afmt.Printf("%s %s\n", color.RedString("Error:"), err.Error())
	}

	cs := readline.NewCancelableStdin(afmt.Stdin)
	go func() {
		<-ctx.Done()
		cs.Close() // nolint:errcheck
	}()

	rl := bufio.NewReader(cs)

uiLoop:
	for {
		// TODO: better exit handling
		if err := ctx.Err(); err != nil {
			return err
		}

		switch state {
		case "import":
			afmt.Print("Data CID (from " + color.YellowString("epik client import") + "): ")

			_cidStr, _, err := rl.ReadLine()
			cidStr := string(_cidStr)
			if err != nil {
				printErr(xerrors.Errorf("reading cid string: %w", err))
				continue
			}

			data, err = cid.Parse(cidStr)
			if err != nil {
				printErr(xerrors.Errorf("parsing cid string: %w", err))
				continue
			}

			color.Blue(".. calculating data size\n")
			ds, err = api.ClientDealPieceCID(ctx, data)
			if err != nil {
				return err
			}

			/* 	state = "duration"
			case "duration":
				afmt.Print("Deal duration (days): ")

				_daystr, _, err := rl.ReadLine()
				daystr := string(_daystr)
				if err != nil {
					return err
				}

				_, err = fmt.Sscan(daystr, &days)
				if err != nil {
					printErr(xerrors.Errorf("parsing duration: %w", err))
					continue
				}

				if days < int(build.MinDealDuration/builtin.EpochsInDay) {
					printErr(xerrors.Errorf("minimum duration is %d days", int(build.MinDealDuration/builtin.EpochsInDay)))
					continue
				}

				dur = 24 * time.Hour * time.Duration(days)
				epochs = abi.ChainEpoch(dur / (time.Duration(build.BlockDelaySecs) * time.Second))

				state = "verified"
			case "verified":
				ts, err := api.ChainHead(ctx)
				if err != nil {
					return err
				}

				dcap, err := api.StateVerifiedClientStatus(ctx, a, ts.Key())
				if err != nil {
					return err
				}

				if dcap == nil {
					state = "miner"
					continue
				}

				if dcap.Uint64() < uint64(ds.PieceSize) {
					color.Yellow(".. not enough DataCap available for a verified deal\n")
					state = "miner"
					continue
				}

				afmt.Print("\nMake this a verified deal? (yes/no): ")

				_yn, _, err := rl.ReadLine()
				yn := string(_yn)
				if err != nil {
					return err
				}

				switch yn {
				case "yes":
					verified = true
				case "no":
					verified = false
				default:
					afmt.Println("Type in full 'yes' or 'no'")
					continue
				}
			*/
			state = "miner"
		case "miner":
			afmt.Print("Miner Addresses (f0.. f0..), none to find: ")

			_maddrsStr, _, err := rl.ReadLine()
			maddrsStr := string(_maddrsStr)
			if err != nil {
				printErr(xerrors.Errorf("reading miner address: %w", err))
				continue
			}

			for _, s := range strings.Fields(maddrsStr) {
				maddr, err := address.NewFromString(strings.TrimSpace(s))
				if err != nil {
					printErr(xerrors.Errorf("parsing miner address: %w", err))
					continue uiLoop
				}

				maddrs = append(maddrs, maddr)
			}

			state = "query"
			if len(maddrs) == 0 {
				state = "find"
			}
		case "find":
			asks, err := GetAsks(ctx, api)
			if err != nil {
				return err
			}

			for _, ask := range asks {
				if ask.Ask.MinPieceSize > ds.PieceSize {
					continue
				}
				if ask.Ask.MaxPieceSize < ds.PieceSize {
					continue
				}
				candidateAsks = append(candidateAsks, ask.Ask)
			}

			afmt.Printf("Found %d candidate asks\n", len(candidateAsks))
			/* state = "find-budget"
			case "find-budget":
				afmt.Printf("Proposing from %s, Current Balance: %s\n", a, types.EPK(fromBal))
				afmt.Print("Maximum budget (EPK): ") // TODO: Propose some default somehow?

				_budgetStr, _, err := rl.ReadLine()
				budgetStr := string(_budgetStr)
				if err != nil {
					printErr(xerrors.Errorf("reading miner address: %w", err))
					continue
				}

				budget, err = types.ParseEPK(budgetStr)
				if err != nil {
					printErr(xerrors.Errorf("parsing EPK: %w", err))
					continue uiLoop
				}

				var goodAsks []*storagemarket.StorageAsk
				for _, ask := range candidateAsks {
					p := ask.Price
					if verified {
						p = ask.VerifiedPrice
					}

					epochPrice := types.BigDiv(types.BigMul(p, types.NewInt(uint64(ds.PieceSize))), gib)
					totalPrice := types.BigMul(epochPrice, types.NewInt(uint64(epochs)))

					if totalPrice.LessThan(abi.TokenAmount(budget)) {
						goodAsks = append(goodAsks, ask)
					}
				}
				candidateAsks = goodAsks
				afmt.Printf("%d asks within budget\n", len(candidateAsks)) */
			state = "find-count"
		case "find-count":
			afmt.Print("Deals to make (1): ")
			dealcStr, _, err := rl.ReadLine()
			if err != nil {
				printErr(xerrors.Errorf("reading deal count: %w", err))
				continue
			}

			dealCount, err = strconv.ParseInt(string(dealcStr), 10, 64)
			if err != nil {
				return err
			}

			color.Blue(".. Picking miners")

			// TODO: some better strategy (this tries to pick randomly)
			var pickedAsks []*storagemarket.StorageAsk
		pickLoop:
			for i := 0; i < 64; i++ {
				rand.Shuffle(len(candidateAsks), func(i, j int) {
					candidateAsks[i], candidateAsks[j] = candidateAsks[j], candidateAsks[i]
				})

				/* remainingBudget := abi.TokenAmount(budget) */
				pickedAsks = []*storagemarket.StorageAsk{}

				for _, ask := range candidateAsks {
					/* p := ask.Price
					if verified {
						p = ask.VerifiedPrice
					}

					epochPrice := types.BigDiv(types.BigMul(p, types.NewInt(uint64(ds.PieceSize))), gib)
					totalPrice := types.BigMul(epochPrice, types.NewInt(uint64(epochs)))

					if totalPrice.GreaterThan(remainingBudget) {
						continue
					} */

					pickedAsks = append(pickedAsks, ask)
					/* remainingBudget = big.Sub(remainingBudget, totalPrice) */

					if len(pickedAsks) == int(dealCount) {
						break pickLoop
					}
				}
			}

			for _, pickedAsk := range pickedAsks {
				maddrs = append(maddrs, pickedAsk.Miner)
				ask = append(ask, *pickedAsk)
			}

			state = "confirm"
		case "query":
			color.Blue(".. querying miner asks")

			for _, maddr := range maddrs {
				mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
				if err != nil {
					printErr(xerrors.Errorf("failed to get peerID for miner: %w", err))
					state = "miner"
					continue uiLoop
				}

				a, err := api.ClientQueryAsk(ctx, *mi.PeerId, maddr)
				if err != nil {
					printErr(xerrors.Errorf("failed to query ask: %w", err))
					state = "miner"
					continue uiLoop
				}

				ask = append(ask, *a)
			}

			// TODO: run more validation
			state = "confirm"
		case "confirm":
			// TODO: do some more or epochs math (round to miner PP, deal start buffer)

			afmt.Printf("-----\n")
			afmt.Printf("Proposing from %s\n", a)
			afmt.Printf("\tBalance: %s\n", types.EPK(fromBal))
			afmt.Printf("\n")
			afmt.Printf("Piece size: %s (Payload size: %s)\n", units.BytesSize(float64(ds.PieceSize)), units.BytesSize(float64(ds.PayloadSize)))
			/* afmt.Printf("Duration: %s\n", dur)

			pricePerGib := big.Zero()
			for _, a := range ask {
				p := a.Price
				if verified {
					p = a.VerifiedPrice
				}
				pricePerGib = big.Add(pricePerGib, p)
				epochPrice := types.BigDiv(types.BigMul(p, types.NewInt(uint64(ds.PieceSize))), gib)
				epochPrices = append(epochPrices, epochPrice)

				mpow, err := api.StateMinerPower(ctx, a.Miner, types.EmptyTSK)
				if err != nil {
					return xerrors.Errorf("getting power (%s): %w", a.Miner, err)
				}

				if len(ask) > 1 {
					totalPrice := types.BigMul(epochPrice, types.NewInt(uint64(epochs)))
					afmt.Printf("Miner %s (Power:%s) price: ~%s (%s per epoch)\n", color.YellowString(a.Miner.String()), color.GreenString(types.SizeStr(mpow.MinerPower.QualityAdjPower)), color.BlueString(types.EPK(totalPrice).String()), types.EPK(epochPrice))
				}
			}

			// TODO: price is based on PaddedPieceSize, right?
			epochPrice := types.BigDiv(types.BigMul(pricePerGib, types.NewInt(uint64(ds.PieceSize))), gib)
			totalPrice := types.BigMul(epochPrice, types.NewInt(uint64(epochs)))

			afmt.Printf("Total price: ~%s (%s per epoch)\n", color.CyanString(types.EPK(totalPrice).String()), types.EPK(epochPrice))
			afmt.Printf("Verified: %v\n", verified) */

			state = "accept"
		case "accept":
			afmt.Print("\nAccept (yes/no): ")

			_yn, _, err := rl.ReadLine()
			yn := string(_yn)
			if err != nil {
				return err
			}

			if yn == "no" {
				return nil
			}

			if yn != "yes" {
				afmt.Println("Type in full 'yes' or 'no'")
				continue
			}

			state = "execute"
		case "execute":
			color.Blue(".. executing\n")

			for _, maddr := range maddrs {
				proposal, err := api.ClientStartDeal(ctx, &lapi.StartDealParams{
					Data: &storagemarket.DataRef{
						TransferType: storagemarket.TTGraphsync,
						Root:         data,

						PieceCid:  &ds.PieceCID,
						PieceSize: ds.PieceSize.Unpadded(),
					},
					Wallet: a,
					Miner:  maddr,
					/* EpochPrice:        epochPrices[i],
					MinBlocksDuration: uint64(epochs), */
					DealStartEpoch: abi.ChainEpoch(cctx.Int64("start-epoch")),
					FastRetrieval:  cctx.Bool("fast-retrieval"),
					/* VerifiedDeal:      verified, */
				})
				if err != nil {
					return err
				}

				encoder, err := GetCidEncoder(cctx)
				if err != nil {
					return err
				}

				afmt.Printf("Deal (%s) CID: %s\n", maddr, color.GreenString(encoder.Encode(*proposal)))
			}

			return nil
		default:
			return xerrors.Errorf("unknown state: %s", state)
		}
	}
}

var clientFindCmd = &cli.Command{
	Name:      "find",
	Usage:     "Find data in the network",
	ArgsUsage: "[dataCid]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "pieceCid",
			Usage: "require data to be retrieved from a specific Piece CID",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			fmt.Println("Usage: find [CID]")
			return nil
		}

		file, err := cid.Parse(cctx.Args().First())
		if err != nil {
			return err
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		// Check if we already have this data locally

		has, err := api.ClientHasLocal(ctx, file)
		if err != nil {
			return err
		}

		if has {
			fmt.Println("LOCAL")
		}

		var pieceCid *cid.Cid
		if cctx.String("pieceCid") != "" {
			parsed, err := cid.Parse(cctx.String("pieceCid"))
			if err != nil {
				return err
			}
			pieceCid = &parsed
		}

		offers, err := api.ClientFindData(ctx, file, pieceCid)
		if err != nil {
			return err
		}

		for _, offer := range offers {
			if offer.Err != "" {
				fmt.Printf("ERR %s@%s: %s\n", offer.Miner, offer.MinerPeer.ID, offer.Err)
				continue
			}
			fmt.Printf("RETRIEVAL %s@%s-%s-%s\n", offer.Miner, offer.MinerPeer.ID, types.EPK(offer.MinPrice), types.SizeStr(types.NewInt(offer.Size)))
		}

		return nil
	},
}

const DefaultMaxRetrievePrice = 1

var clientRetrieveCmd = &cli.Command{
	Name:      "retrieve",
	Usage:     "Retrieve data from network",
	ArgsUsage: "[dataCid outputPath]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address to send transactions from",
		},
		&cli.BoolFlag{
			Name:  "car",
			Usage: "export to a car file instead of a regular file",
		},
		&cli.StringFlag{
			Name:  "miner",
			Usage: "miner address for retrieval, if not present it'll use local discovery",
		},
		&cli.StringFlag{
			Name:  "maxPrice",
			Usage: fmt.Sprintf("maximum price the client is willing to consider (default: %d EPK)", DefaultMaxRetrievePrice),
		},
		&cli.StringFlag{
			Name:  "pieceCid",
			Usage: "require data to be retrieved from a specific Piece CID",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() != 2 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		fapi, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)
		afmt := NewAppFmt(cctx.App)

		var payer address.Address
		if cctx.String("from") != "" {
			payer, err = address.NewFromString(cctx.String("from"))
		} else {
			payer, err = fapi.WalletDefaultAddress(ctx)
		}
		if err != nil {
			return err
		}

		file, err := cid.Parse(cctx.Args().Get(0))
		if err != nil {
			return err
		}

		// Check if we already have this data locally

		/*has, err := api.ClientHasLocal(ctx, file)
		if err != nil {
			return err
		}

		if has {
			fmt.Println("Success: Already in local storage")
			return nil
		}*/ // TODO: fix

		var pieceCid *cid.Cid
		if cctx.String("pieceCid") != "" {
			parsed, err := cid.Parse(cctx.String("pieceCid"))
			if err != nil {
				return err
			}
			pieceCid = &parsed
		}

		var offer api.QueryOffer
		minerStrAddr := cctx.String("miner")
		if minerStrAddr == "" { // Local discovery
			offers, err := fapi.ClientFindData(ctx, file, pieceCid)

			var cleaned []api.QueryOffer
			// filter out offers that errored
			for _, o := range offers {
				if o.Err == "" {
					cleaned = append(cleaned, o)
				}
			}

			offers = cleaned

			// sort by price low to high
			sort.Slice(offers, func(i, j int) bool {
				return offers[i].MinPrice.LessThan(offers[j].MinPrice)
			})
			if err != nil {
				return err
			}

			// TODO: parse offer strings from `client find`, make this smarter
			if len(offers) < 1 {
				fmt.Println("Failed to find file")
				return nil
			}
			offer = offers[0]
		} else { // Directed retrieval
			minerAddr, err := address.NewFromString(minerStrAddr)
			if err != nil {
				return err
			}
			offer, err = fapi.ClientMinerQueryOffer(ctx, minerAddr, file, pieceCid)
			if err != nil {
				return err
			}
		}
		if offer.Err != "" {
			return fmt.Errorf("The received offer errored: %s", offer.Err)
		}

		maxPrice := types.FromEpk(DefaultMaxRetrievePrice)

		if cctx.String("maxPrice") != "" {
			maxPriceFil, err := types.ParseEPK(cctx.String("maxPrice"))
			if err != nil {
				return xerrors.Errorf("parsing maxPrice: %w", err)
			}

			maxPrice = types.BigInt(maxPriceFil)
		}

		if offer.MinPrice.GreaterThan(maxPrice) {
			return xerrors.Errorf("failed to find offer satisfying maxPrice: %s", maxPrice)
		}

		ref := &lapi.FileRef{
			Path:  cctx.Args().Get(1),
			IsCAR: cctx.Bool("car"),
		}
		updates, err := fapi.ClientRetrieveWithEvents(ctx, offer.Order(payer), ref)
		if err != nil {
			return xerrors.Errorf("error setting up retrieval: %w", err)
		}

		for {
			select {
			case evt, ok := <-updates:
				if ok {
					afmt.Printf("> Recv: %s, %s (%s)\n",
						types.SizeStr(types.NewInt(evt.BytesReceived)),
						retrievalmarket.ClientEvents[evt.Event],
						retrievalmarket.DealStatuses[evt.Status],
					)
				} else {
					afmt.Println("Success")
					return nil
				}

				if evt.Err != "" {
					return xerrors.Errorf("retrieval failed: %s", evt.Err)
				}
			case <-ctx.Done():
				return xerrors.Errorf("retrieval timed out")
			}
		}
	},
}

var clientRetrieveDealCmd = &cli.Command{
	Name:      "retrieve-deal",
	Usage:     "retrieve deal info",
	ArgsUsage: "[dealID]",
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() != 1 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		dealID, err := strconv.ParseUint(cctx.Args().Get(0), 10, 64)
		if err != nil {
			return err
		}
		deal, err := api.ClientRetrieveGetDeal(ctx, retrievalmarket.DealID(dealID))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to get retrival deal: %w", err))
		}
		fmt.Printf("Retrieve ID: %d\n", deal.DealID)
		fmt.Printf("Retrieve RootID: %s\n", deal.RootCID)
		fmt.Printf("Retrieve PieceID: %s\n", deal.PieceCID)
		fmt.Printf("Retrieve Client: %s\n", deal.ClientWallet)
		fmt.Printf("Retrieve Miner: %s\n", deal.MinerWallet)
		fmt.Printf("Retrieve Status: %s\n", retrievalmarket.DealStatuses[deal.Status])
		fmt.Printf("Retrieve Message: %s\n", deal.Message)
		if deal.WaitMsgCID != nil {
			fmt.Printf("Retrieve WaitMsgCID: %s\n", deal.WaitMsgCID.String())
		}
		return nil
	},
}

var clientRetrieveListCmd = &cli.Command{
	Name:      "retrieve-list",
	Usage:     "list retrieval deals",
	ArgsUsage: "",
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		deals, err := api.ClientRetrieveListDeals(ctx)
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to get retrival list: %w", err))
		}

		fmt.Fprintf(cctx.App.Writer, "\nRetrieve lists\n\n")
		w := tablewriter.New(tablewriter.Col("DealId"),
			tablewriter.Col("RootID"),
			tablewriter.Col("PieceCID"),
			// tablewriter.Col("Client"),
			tablewriter.Col("Provider"),
			tablewriter.Col("Status"),
			tablewriter.NewLineCol("Message"))

		for _, d := range deals {
			w.Write(map[string]interface{}{
				"DealId":   d.DealID,
				"RootID":   d.RootCID,
				"PieceCID": d.PieceCID,
				// "Client":   d.ClientWallet,
				"Provider": d.MinerWallet,
				"Status":   retrievalmarket.DealStatuses[d.Status],
				"Message":  d.Message,
			})
		}

		return w.Flush(cctx.App.Writer)
	},
}

var clientRetrievePledgeCmd = &cli.Command{
	Name:      "retrieve-pledge",
	Usage:     "pledge amount for retrieval",
	ArgsUsage: "[amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address to send transactions from",
		},
		&cli.StringFlag{
			Name:  "target",
			Usage: "address to receive the pledge, default is same with the from address.",
		},
		&cli.StringFlag{
			Name:  "miner",
			Usage: "address to bind miner",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() != 1 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		targertAddr := fromAddr
		if target := cctx.String("target"); target != "" {
			addr, err := address.NewFromString(target)
			if err != nil {
				return err
			}

			targertAddr = addr
		}

		miners := []address.Address{}
		if minerStr := cctx.String("miner"); minerStr != "" {
			addrs := strings.Split(minerStr, ",")
			for _, addr := range addrs {
				miner, err := address.NewFromString(addr)
				if err != nil {
					return err
				}
				miners = append(miners, miner)
			}
		}

		val, err := types.ParseEPK(cctx.Args().Get(0))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to parse amount: %w", err))
		}
		msg, err := api.ClientRetrievePledge(ctx, fromAddr, targertAddr, miners, big.Int(val))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to pledge retrival: %w", err))
		}
		fmt.Printf("retrieve pledge: %s\n", msg)
		return nil
	},
}

var clientRetrieveBindCmd = &cli.Command{
	Name:      "retrieve-bind",
	Usage:     "bind miners for storage retrieval",
	ArgsUsage: "[miners ...]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address to send transactions from",
		},
		&cli.BoolFlag{
			Name:  "reverse",
			Usage: "reverse for unbind miners",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() < 1 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}
		reverse := cctx.Bool("reverse")
		miners := []address.Address{}
		for _, m := range cctx.Args().Slice() {
			miner, err := address.NewFromString(m)
			if err != nil {
				return err
			}
			miners = append(miners, miner)
		}
		msg, err := api.ClientRetrieveBind(ctx, fromAddr, miners, reverse)
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to pledge retrival: %w", err))
		}
		fmt.Printf("retrieve pledge: %s\n", msg)
		return nil
	},
}

var clientRetrieveMinerCmd = &cli.Command{
	Name:      "retrieve-miner-assign",
	Usage:     "miner assign pledger for storage retrieval",
	ArgsUsage: "[miner pledgerAddress]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address for the miner owner",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() < 2 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		// from
		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}
		maddr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return err
		}

		pledger, err := address.NewFromString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		params, err := actors.SerializeParams(&miner2.RetrievalPledgeParams{
			Pledger: pledger, // Default to attempting to withdraw all the extra funds in the miner actor
		})
		if err != nil {
			return err
		}

		msg, err := api.MpoolPushMessage(ctx, &types.Message{
			From:   fromAddr,
			To:     maddr,
			Value:  abi.NewTokenAmount(0),
			Method: miner.Methods.BindRetrievalPledger,
			Params: params,
		}, nil)
		if err != nil {
			return err
		}

		fmt.Printf("retrieve miner assign: %s\n", msg.Cid())
		return nil
	},
}

var clientRetrieveInfoCmd = &cli.Command{
	Name:      "retrieve-info",
	Usage:     "global info for retrieval",
	ArgsUsage: "query pledge info",
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		state, err := api.StateRetrievalInfo(ctx, types.EmptyTSK)
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to query state: %w", err))
		}

		fmt.Printf("Retrieve Total Pledge: %s\n", types.EPK(state.TotalPledge).String())
		fmt.Printf("Retrieve Total Reward: %s\n", types.EPK(state.TotalReward).String())
		fmt.Printf("Retrieve Pending Reward: %s\n", types.EPK(state.PendingReward).String())
		return nil
	},
}

var clientRetrievePledgeStateCmd = &cli.Command{
	Name:      "retrieve-state",
	Usage:     "pledge state for retrieval",
	ArgsUsage: "query pledge state",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address to send transactions from",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
			fmt.Printf("Retrieve pledge address: %s\n", fromAddr)
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		state, _ := api.StateRetrievalPledge(ctx, fromAddr, types.EmptyTSK)

		pledges, _ := api.StateRetrievalPledgeFrom(ctx, fromAddr, types.EmptyTSK)

		if state != nil {
			fmt.Printf("Retrieve State:\n")
			fmt.Printf("Retrieve Balance: %s\n", types.EPK(state.Balance).String())
			fmt.Printf("Retrieve Day expend: %s\n", types.EPK(state.DayExpend).String())
			if len(state.BindMiners) > 0 {
				miners := ""
				for _, m := range state.BindMiners {
					miners = miners + "," + m.String()
				}
				miners = strings.TrimLeft(miners, ",")
				fmt.Printf("Bind Miners: %s\n", miners)
			}
		}

		if pledges != nil {
			fmt.Printf("Retrieve Pledge:\n")
			fmt.Printf("Locked Balance: %s\n", types.EPK(pledges.Locked).String())
			fmt.Printf("Unlocked Epoch: %d\n", pledges.UnlockedEpoch)
			if len(pledges.Pledges) > 0 {
				fmt.Printf("Pledge:\n")
				for target, token := range pledges.Pledges {
					fmt.Printf("%s: %s\n", target, types.EPK(token).String())
				}
			}
		}
		return nil
	},
}

var clientRetrieveApplyForWithdrawCmd = &cli.Command{
	Name:      "retrieve-apply",
	Usage:     "apply for withdraw amount for retrieval",
	ArgsUsage: "[amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address to send transactions from",
		},
		&cli.StringFlag{
			Name:  "target",
			Usage: "address of pledge target",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() != 1 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		target := fromAddr
		if tstr := cctx.String("target"); tstr != "" {
			addr, err := address.NewFromString(tstr)
			if err != nil {
				return err
			}

			target = addr
		}

		val, err := types.ParseEPK(cctx.Args().Get(0))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to parse amount: %w", err))
		}
		msg, err := api.ClientRetrieveApplyForWithdraw(ctx, fromAddr, target, big.Int(val))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to apply for withdraw: %w", err))
		}
		fmt.Printf("retrieve apply for withdraw: %s\n", msg)
		return nil
	},
}

var clientRetrieveWithdrawCmd = &cli.Command{
	Name:      "retrieve-withdraw",
	Usage:     "withdraw amount for retrieval",
	ArgsUsage: "[amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "address to send transactions from",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() != 1 {
			return ShowHelp(cctx, fmt.Errorf("incorrect number of arguments"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		val, err := types.ParseEPK(cctx.Args().Get(0))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to parse amount: %w", err))
		}
		msg, err := api.ClientRetrieveWithdraw(ctx, fromAddr, big.Int(val))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to withdraw retrival: %w", err))
		}
		fmt.Printf("retrieve withdraw: %s\n", msg)
		return nil
	},
}

var clientDealStatsCmd = &cli.Command{
	Name:  "deal-stats",
	Usage: "Print statistics about local storage deals",
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name: "newer-than",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		localDeals, err := api.ClientListDeals(ctx)
		if err != nil {
			return err
		}

		var totalSize uint64
		byState := map[storagemarket.StorageDealStatus][]uint64{}
		for _, deal := range localDeals {
			if cctx.IsSet("newer-than") {
				if time.Now().Sub(deal.CreationTime) > cctx.Duration("newer-than") {
					continue
				}
			}

			totalSize += deal.Size
			byState[deal.State] = append(byState[deal.State], deal.Size)
		}

		fmt.Printf("Total: %d deals, %s\n", len(localDeals), types.SizeStr(types.NewInt(totalSize)))

		type stateStat struct {
			state storagemarket.StorageDealStatus
			count int
			bytes uint64
		}

		stateStats := make([]stateStat, 0, len(byState))
		for state, deals := range byState {
			if state == storagemarket.StorageDealActive {
				state = math.MaxUint64 // for sort
			}

			st := stateStat{
				state: state,
				count: len(deals),
			}
			for _, b := range deals {
				st.bytes += b
			}

			stateStats = append(stateStats, st)
		}

		sort.Slice(stateStats, func(i, j int) bool {
			return int64(stateStats[i].state) < int64(stateStats[j].state)
		})

		for _, st := range stateStats {
			if st.state == math.MaxUint64 {
				st.state = storagemarket.StorageDealActive
			}
			fmt.Printf("%s: %d deals, %s\n", storagemarket.DealStates[st.state], st.count, types.SizeStr(types.NewInt(st.bytes)))
		}

		return nil
	},
}

var miningPledgeCmd = &cli.Command{
	Name:  "mining-pledge",
	Usage: "Manipulate pledge funds for mining",
	Subcommands: []*cli.Command{
		miningPledgeAddCmd,
		miningPledgeApplyForWithdrawCmd,
		miningPledgeWithdrawCmd,
		miningPledgeTransferCmd,
	},
}

var miningPledgeAddCmd = &cli.Command{
	Name:      "add",
	Usage:     "Add pledge funds",
	ArgsUsage: "[minerAddress] [amount (EPK)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return xerrors.New("'add' expects two arguments, address and amount")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		f, err := types.ParseEPK(cctx.Args().Get(1))
		if err != nil {
			return xerrors.Errorf("parsing 'amount' argument: %w", err)
		}
		amount := abi.TokenAmount(f)

		// from
		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		smsg, err := api.MpoolPushMessage(ctx, &types.Message{
			From:   fromAddr,
			To:     maddr,
			Value:  amount,
			Method: miner.Methods.AddPledge,
			Params: nil,
		}, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Sent 'mining-pledge add' message %s\n", smsg.Cid())

		return nil
	},
}

var miningPledgeApplyForWithdrawCmd = &cli.Command{
	Name:      "applyfor-withdraw",
	Usage:     "apply for Withdraw pledge funds",
	ArgsUsage: "[minerAddress] [amount (EPK, optional)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.New("'applyfor-withdraw' expects at least one argument, miner address")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		// from
		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		funds, err := api.StateMinerFunds(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		totalAmount := funds.MiningPledge
		if len(funds.MiningPledgeLocked) > 0 {
			for _, l := range funds.MiningPledgeLocked {
				totalAmount = big.Add(totalAmount, l.Amount)
			}
		}
		if totalAmount.IsZero() {
			return xerrors.New("no pledge funds")
		}

		reqAmount := totalAmount
		if cctx.Args().Len() > 1 {
			arg1, err := types.ParseEPK(cctx.Args().Get(1))
			if err != nil {
				return xerrors.Errorf("parsing 'amount' argument: %w", err)
			}
			if abi.TokenAmount(arg1).GreaterThan(totalAmount) {
				return xerrors.Errorf("pledge balance %s less than requested: %s", types.EPK(totalAmount), types.EPK(arg1))
			}
			reqAmount = abi.TokenAmount(arg1)
		}

		params, err := actors.SerializeParams(&miner2.WithdrawPledgeParams{
			AmountRequested: reqAmount, // Default to attempting to withdraw all the extra funds in the miner actor
		})
		if err != nil {
			return err
		}

		smsg, err := api.MpoolPushMessage(ctx, &types.Message{
			To:     maddr,
			From:   fromAddr,
			Value:  types.NewInt(0),
			Method: miner.Methods.ApplyForWithdraw,
			Params: params,
		}, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Sent 'mining-pledge apply for withdraw' %s\n", smsg.Cid())

		return nil
	},
}

var miningPledgeWithdrawCmd = &cli.Command{
	Name:      "withdraw",
	Usage:     "Withdraw pledge funds",
	ArgsUsage: "[minerAddress] [amount (EPK, optional)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.New("'withdraw' expects at least one argument, miner address")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		// from
		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		funds, err := api.StateMinerFunds(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}
		totalAmount := funds.MiningPledge
		if len(funds.MiningPledgeLocked) > 0 {
			for _, l := range funds.MiningPledgeLocked {
				totalAmount = big.Add(totalAmount, l.Amount)
			}
		}
		if totalAmount.IsZero() {
			return xerrors.New("no pledge funds")
		}

		reqAmount := totalAmount
		if cctx.Args().Len() > 1 {
			arg1, err := types.ParseEPK(cctx.Args().Get(1))
			if err != nil {
				return xerrors.Errorf("parsing 'amount' argument: %w", err)
			}
			if abi.TokenAmount(arg1).GreaterThan(totalAmount) {
				return xerrors.Errorf("pledge balance %s less than requested: %s", types.EPK(totalAmount), types.EPK(arg1))
			}
			reqAmount = abi.TokenAmount(arg1)
		}

		params, err := actors.SerializeParams(&miner2.WithdrawPledgeParams{
			AmountRequested: reqAmount, // Default to attempting to withdraw all the extra funds in the miner actor
		})
		if err != nil {
			return err
		}

		smsg, err := api.MpoolPushMessage(ctx, &types.Message{
			To:     maddr,
			From:   fromAddr,
			Value:  types.NewInt(0),
			Method: miner.Methods.WithdrawPledge,
			Params: params,
		}, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Sent 'mining-pledge withdraw' %s\n", smsg.Cid())

		return nil
	},
}

var miningPledgeTransferCmd = &cli.Command{
	Name:      "transfer",
	Usage:     "transfer pledge funds from miner to target miner.",
	ArgsUsage: "[minerAddress] [targetMiner] [amount (EPK, optional)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 2 {
			return xerrors.New("'transfer' expects at least two argument, miner address and target miner")
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		targetMiner, err := address.NewFromString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		// from
		var fromAddr address.Address
		if from := cctx.String("from"); from == "" {
			defaddr, err := api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}

			fromAddr = defaddr
		} else {
			addr, err := address.NewFromString(from)
			if err != nil {
				return err
			}

			fromAddr = addr
		}

		funds, err := api.StateMinerFunds(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		totalAmount := funds.MiningPledge
		if len(funds.MiningPledgeLocked) > 0 {
			for _, l := range funds.MiningPledgeLocked {
				totalAmount = big.Add(totalAmount, l.Amount)
			}
		}
		if totalAmount.IsZero() {
			return xerrors.New("no pledge funds")
		}

		reqAmount := totalAmount
		if cctx.Args().Len() > 2 {
			arg2, err := types.ParseEPK(cctx.Args().Get(2))
			if err != nil {
				return xerrors.Errorf("parsing 'amount' argument: %w", err)
			}
			if abi.TokenAmount(arg2).GreaterThan(totalAmount) {
				return xerrors.Errorf("pledge balance %s less than requested: %s", types.EPK(totalAmount), types.EPK(arg2))
			}
			reqAmount = abi.TokenAmount(arg2)
		}

		params, err := actors.SerializeParams(&miner2.TransferPledgeParamsV2{
			Amount: reqAmount,
			Miner:  targetMiner,
		})
		if err != nil {
			return err
		}

		smsg, err := api.MpoolPushMessage(ctx, &types.Message{
			To:     maddr,
			From:   fromAddr,
			Value:  types.NewInt(0),
			Method: miner.Methods.TransferPledgeV2,
			Params: params,
		}, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Sent 'mining-pledge transfer' %s\n", smsg.Cid())

		return nil
	},
}

var clientListAsksCmd = &cli.Command{
	Name:  "list-asks",
	Usage: "List asks for top miners",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "by-ping",
		},
		&cli.StringFlag{
			Name:  "output-format",
			Value: "text",
			Usage: "Either 'text' or 'csv'",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		asks, err := GetAsks(ctx, api)
		if err != nil {
			return err
		}

		if cctx.Bool("by-ping") {
			sort.Slice(asks, func(i, j int) bool {
				return asks[i].Ping < asks[j].Ping
			})
		}
		pfmt := "%s: min:%s max:%s ping:%s\n"
		if cctx.String("output-format") == "csv" {
			fmt.Printf("Miner,Min,Max,Ping\n")
			pfmt = "%s,%s,%s,%s\n"
		}

		for _, a := range asks {
			ask := a.Ask

			fmt.Printf(pfmt, ask.Miner,
				types.SizeStr(types.NewInt(uint64(ask.MinPieceSize))),
				types.SizeStr(types.NewInt(uint64(ask.MaxPieceSize))),
				// types.EPK(ask.Price),
				// types.EPK(ask.VerifiedPrice),
				a.Ping,
			)
		}

		return nil
	},
}

type QueriedAsk struct {
	Ask  *storagemarket.StorageAsk
	Ping time.Duration
}

func GetAsks(ctx context.Context, api lapi.FullNode) ([]QueriedAsk, error) {
	isTTY := true
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		isTTY = false
	}
	if isTTY {
		color.Blue(".. getting miner list")
	}
	miners, err := api.StateListMiners(ctx, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting miner list: %w", err)
	}

	var lk sync.Mutex
	var found int64
	var withMinPower []address.Address
	done := make(chan struct{})

	go func() {
		defer close(done)

		var wg sync.WaitGroup
		wg.Add(len(miners))

		throttle := make(chan struct{}, 50)
		for _, miner := range miners {
			throttle <- struct{}{}
			go func(miner address.Address) {
				defer wg.Done()
				defer func() {
					<-throttle
				}()

				power, err := api.StateMinerPower(ctx, miner, types.EmptyTSK)
				if err != nil {
					return
				}

				if power.HasMinPower { // TODO: Lower threshold
					atomic.AddInt64(&found, 1)
					lk.Lock()
					withMinPower = append(withMinPower, miner)
					lk.Unlock()
				}
			}(miner)
		}
	}()

loop:
	for {
		select {
		case <-time.After(150 * time.Millisecond):
			if isTTY {
				fmt.Printf("\r* Found %d miners with power", atomic.LoadInt64(&found))
			}
		case <-done:
			break loop
		}
	}
	if isTTY {
		fmt.Printf("\r* Found %d miners with power\n", atomic.LoadInt64(&found))

		color.Blue(".. querying asks")
	}

	var asks []QueriedAsk
	var queried, got int64

	done = make(chan struct{})
	go func() {
		defer close(done)

		var wg sync.WaitGroup
		wg.Add(len(withMinPower))

		throttle := make(chan struct{}, 50)
		for _, miner := range withMinPower {
			throttle <- struct{}{}
			go func(miner address.Address) {
				defer wg.Done()
				defer func() {
					<-throttle
					atomic.AddInt64(&queried, 1)
				}()

				ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
				defer cancel()

				mi, err := api.StateMinerInfo(ctx, miner, types.EmptyTSK)
				if err != nil {
					return
				}
				if mi.PeerId == nil {
					return
				}

				ask, err := api.ClientQueryAsk(ctx, *mi.PeerId, miner)
				if err != nil {
					return
				}

				rt := time.Now()

				_, err = api.ClientQueryAsk(ctx, *mi.PeerId, miner)
				if err != nil {
					return
				}

				atomic.AddInt64(&got, 1)
				lk.Lock()
				asks = append(asks, QueriedAsk{
					Ask:  ask,
					Ping: time.Now().Sub(rt),
				})
				lk.Unlock()
			}(miner)
		}
	}()

loop2:
	for {
		select {
		case <-time.After(150 * time.Millisecond):
			if isTTY {
				fmt.Printf("\r* Queried %d asks, got %d responses", atomic.LoadInt64(&queried), atomic.LoadInt64(&got))
			}
		case <-done:
			break loop2
		}
	}
	if isTTY {
		fmt.Printf("\r* Queried %d asks, got %d responses\n", atomic.LoadInt64(&queried), atomic.LoadInt64(&got))
	}

	/* sort.Slice(asks, func(i, j int) bool {
		return asks[i].Ask.Price.LessThan(asks[j].Ask.Price)
	}) */

	return asks, nil
}

var clientQueryAskCmd = &cli.Command{
	Name:      "query-ask",
	Usage:     "Find a miners ask",
	ArgsUsage: "[minerAddress]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "peerid",
			Usage: "specify peer ID of node to make query against",
		},
		&cli.Int64Flag{
			Name:  "size",
			Usage: "data size in bytes",
		},
		/* &cli.Int64Flag{
			Name:  "duration",
			Usage: "deal duration",
		}, */
	},
	Action: func(cctx *cli.Context) error {
		afmt := NewAppFmt(cctx.App)
		if cctx.NArg() != 1 {
			afmt.Println("Usage: query-ask [minerAddress]")
			return nil
		}

		maddr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var pid peer.ID
		if pidstr := cctx.String("peerid"); pidstr != "" {
			p, err := peer.Decode(pidstr)
			if err != nil {
				return err
			}
			pid = p
		} else {
			mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("failed to get peerID for miner: %w", err)
			}

			if *mi.PeerId == peer.ID("SETME") {
				return fmt.Errorf("the miner hasn't initialized yet")
			}

			pid = *mi.PeerId
		}

		ask, err := api.ClientQueryAsk(ctx, pid, maddr)
		if err != nil {
			return err
		}

		afmt.Printf("Ask: %s\n", maddr)
		/* afmt.Printf("Price per GiB: %s\n", types.EPK(ask.Price))
		afmt.Printf("Verified Price per GiB: %s\n", types.EPK(ask.VerifiedPrice)) */
		afmt.Printf("Max Piece size: %s\n", types.SizeStr(types.NewInt(uint64(ask.MaxPieceSize))))
		afmt.Printf("Min Piece size: %s\n", types.SizeStr(types.NewInt(uint64(ask.MinPieceSize))))

		size := cctx.Int64("size")
		if size == 0 {
			return nil
		}
		/* perEpoch := types.BigDiv(types.BigMul(ask.Price, types.NewInt(uint64(size))), types.NewInt(1<<30))
		afmt.Printf("Price per Block: %s\n", types.EPK(perEpoch))

		duration := cctx.Int64("duration")
		if duration == 0 {
			return nil
		}
		afmt.Printf("Total Price: %s\n", types.EPK(types.BigMul(perEpoch, types.NewInt(uint64(duration))))) */

		return nil
	},
}

var clientListDeals = &cli.Command{
	Name:  "list-deals",
	Usage: "List storage market deals",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "print verbose deal details",
		},
		&cli.BoolFlag{
			Name:  "color",
			Usage: "use color in display output",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "show-failed",
			Usage: "show failed/failing deals",
		},
		&cli.BoolFlag{
			Name:  "watch",
			Usage: "watch deal updates in real-time, rather than a one time list",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		verbose := cctx.Bool("verbose")
		color := cctx.Bool("color")
		watch := cctx.Bool("watch")
		showFailed := cctx.Bool("show-failed")

		localDeals, err := api.ClientListDeals(ctx)
		if err != nil {
			return err
		}

		if watch {
			updates, err := api.ClientGetDealUpdates(ctx)
			if err != nil {
				return err
			}

			for {
				tm.Clear()
				tm.MoveCursor(1, 1)

				err = outputStorageDeals(ctx, tm.Screen, api, localDeals, verbose, color, showFailed)
				if err != nil {
					return err
				}

				tm.Flush()

				select {
				case <-ctx.Done():
					return nil
				case updated := <-updates:
					var found bool
					for i, existing := range localDeals {
						if existing.ProposalCid.Equals(updated.ProposalCid) {
							localDeals[i] = updated
							found = true
							break
						}
					}
					if !found {
						localDeals = append(localDeals, updated)
					}
				}
			}
		}

		return outputStorageDeals(ctx, cctx.App.Writer, api, localDeals, verbose, color, showFailed)
	},
}

func dealFromDealInfo(ctx context.Context, full api.FullNode, head *types.TipSet, v api.DealInfo) deal {
	if v.DealID == 0 {
		return deal{
			LocalDeal:        v,
			OnChainDealState: *market.EmptyDealState(),
		}
	}

	onChain, err := full.StateMarketStorageDeal(ctx, v.DealID, head.Key())
	if err != nil {
		return deal{LocalDeal: v}
	}

	return deal{
		LocalDeal:        v,
		OnChainDealState: onChain.State,
	}
}

func outputStorageDeals(ctx context.Context, out io.Writer, full lapi.FullNode, localDeals []lapi.DealInfo, verbose bool, color bool, showFailed bool) error {
	sort.Slice(localDeals, func(i, j int) bool {
		return localDeals[i].CreationTime.Before(localDeals[j].CreationTime)
	})

	head, err := full.ChainHead(ctx)
	if err != nil {
		return err
	}

	var deals []deal
	for _, localDeal := range localDeals {
		if showFailed || localDeal.State != storagemarket.StorageDealError {
			deals = append(deals, dealFromDealInfo(ctx, full, head, localDeal))
		}
	}

	if verbose {
		w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintf(w, "Created\tDealCid\tDealId\tProvider\tState\tOn Chain?\tSlashed?\tPieceCID\tSize\tTransferChannelID\tTransferStatus\tMessage\n")
		for _, d := range deals {
			onChain := "N"
			if d.OnChainDealState.SectorStartEpoch != -1 {
				onChain = fmt.Sprintf("Y (epoch %d)", d.OnChainDealState.SectorStartEpoch)
			}

			slashed := "N"
			if d.OnChainDealState.SlashEpoch != -1 {
				slashed = fmt.Sprintf("Y (epoch %d)", d.OnChainDealState.SlashEpoch)
			}

			// price := types.EPK(types.BigMul(d.LocalDeal.PricePerEpoch, types.NewInt(d.LocalDeal.Duration)))
			transferChannelID := ""
			if d.LocalDeal.TransferChannelID != nil {
				transferChannelID = d.LocalDeal.TransferChannelID.String()
			}
			transferStatus := ""
			if d.LocalDeal.DataTransfer != nil {
				transferStatus = datatransfer.Statuses[d.LocalDeal.DataTransfer.Status]
				// TODO: Include the transferred percentage once this bug is fixed:
				// https://github.com/ipfs/go-graphsync/issues/126
				//fmt.Printf("transferred: %d / size: %d\n", d.LocalDeal.DataTransfer.Transferred, d.LocalDeal.Size)
				//if d.LocalDeal.Size > 0 {
				//	pct := (100 * d.LocalDeal.DataTransfer.Transferred) / d.LocalDeal.Size
				//	transferPct = fmt.Sprintf("%d%%", pct)
				//}
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				d.LocalDeal.CreationTime.Format(time.Stamp),
				d.LocalDeal.ProposalCid,
				d.LocalDeal.DealID,
				d.LocalDeal.Provider,
				dealStateString(color, d.LocalDeal.State),
				onChain,
				slashed,
				d.LocalDeal.PieceCID,
				types.SizeStr(types.NewInt(d.LocalDeal.Size)),
				// price,
				// d.LocalDeal.Duration,
				transferChannelID,
				transferStatus,
				// d.LocalDeal.Verified,
				d.LocalDeal.Message)
		}
		return w.Flush()
	}

	w := tablewriter.New(tablewriter.Col("DealCid"),
		tablewriter.Col("DealId"),
		tablewriter.Col("Provider"),
		tablewriter.Col("State"),
		tablewriter.Col("On Chain?"),
		tablewriter.Col("Slashed?"),
		tablewriter.Col("PieceCID"),
		tablewriter.Col("Size"),
		tablewriter.Col("Price"),
		tablewriter.Col("Duration"),
		tablewriter.Col("Verified"),
		tablewriter.NewLineCol("Message"))

	for _, d := range deals {
		propcid := ellipsis(d.LocalDeal.ProposalCid.String(), 8)

		onChain := "N"
		if d.OnChainDealState.SectorStartEpoch != -1 {
			onChain = fmt.Sprintf("Y (epoch %d)", d.OnChainDealState.SectorStartEpoch)
		}

		slashed := "N"
		if d.OnChainDealState.SlashEpoch != -1 {
			slashed = fmt.Sprintf("Y (epoch %d)", d.OnChainDealState.SlashEpoch)
		}

		piece := ellipsis(d.LocalDeal.PieceCID.String(), 8)

		/* price := types.EPK(types.BigMul(d.LocalDeal.PricePerEpoch, types.NewInt(d.LocalDeal.Duration))) */

		w.Write(map[string]interface{}{
			"DealCid":   propcid,
			"DealId":    d.LocalDeal.DealID,
			"Provider":  d.LocalDeal.Provider,
			"State":     dealStateString(color, d.LocalDeal.State),
			"On Chain?": onChain,
			"Slashed?":  slashed,
			"PieceCID":  piece,
			"Size":      types.SizeStr(types.NewInt(d.LocalDeal.Size)),
			/* "Price":     price,
			"Verified":  d.LocalDeal.Verified,
			"Duration":  d.LocalDeal.Duration, */
			"Message": d.LocalDeal.Message,
		})
	}

	return w.Flush(out)
}

func dealStateString(c bool, state storagemarket.StorageDealStatus) string {
	s := storagemarket.DealStates[state]
	if !c {
		return s
	}

	switch state {
	case storagemarket.StorageDealError, storagemarket.StorageDealExpired:
		return color.RedString(s)
	case storagemarket.StorageDealActive:
		return color.GreenString(s)
	default:
		return s
	}
}

type deal struct {
	LocalDeal        lapi.DealInfo
	OnChainDealState market.DealState
}

var clientGetDealCmd = &cli.Command{
	Name:  "get-deal",
	Usage: "Print detailed deal information",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		propcid, err := cid.Decode(cctx.Args().First())
		if err != nil {
			return err
		}

		di, err := api.ClientGetDealInfo(ctx, propcid)
		if err != nil {
			return err
		}

		out := map[string]interface{}{
			"DealInfo: ": di,
		}

		if di.DealID != 0 {
			onChain, err := api.StateMarketStorageDeal(ctx, di.DealID, types.EmptyTSK)
			if err != nil {
				return err
			}

			out["OnChain"] = onChain
		}

		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	},
}

// var clientBalancesCmd = &cli.Command{
// 	Name:  "balances",
// 	Usage: "Print storage market client balances",
// 	Flags: []cli.Flag{
// 		&cli.StringFlag{
// 			Name:  "client",
// 			Usage: "specify storage client address",
// 		},
// 	},
// 	Action: func(cctx *cli.Context) error {
// 		api, closer, err := GetFullNodeAPI(cctx)
// 		if err != nil {
// 			return err
// 		}
// 		defer closer()
// 		ctx := ReqContext(cctx)

// 		var addr address.Address
// 		if clientFlag := cctx.String("client"); clientFlag != "" {
// 			ca, err := address.NewFromString(clientFlag)
// 			if err != nil {
// 				return err
// 			}

// 			addr = ca
// 		} else {
// 			def, err := api.WalletDefaultAddress(ctx)
// 			if err != nil {
// 				return err
// 			}
// 			addr = def
// 		}

// 		balance, err := api.StateMarketBalance(ctx, addr, types.EmptyTSK)
// 		if err != nil {
// 			return err
// 		}

// 		reserved, err := api.MarketGetReserved(ctx, addr)
// 		if err != nil {
// 			return err
// 		}

// 		avail := big.Sub(big.Sub(balance.Escrow, balance.Locked), reserved)
// 		if avail.LessThan(big.Zero()) {
// 			avail = big.Zero()
// 		}

// 		fmt.Printf("Client Market Balance for address %s:\n", addr)

// 		fmt.Printf("  Escrowed Funds:        %s\n", types.EPK(balance.Escrow))
// 		fmt.Printf("  Locked Funds:          %s\n", types.EPK(balance.Locked))
// 		fmt.Printf("  Reserved Funds:        %s\n", types.EPK(reserved))
// 		fmt.Printf("  Available to Withdraw: %s\n", types.EPK(avail))

// 		return nil
// 	},
// }

var clientStat = &cli.Command{
	Name:      "stat",
	Usage:     "Print information about a locally stored file (piece size, etc)",
	ArgsUsage: "<cid>",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() || cctx.NArg() != 1 {
			return fmt.Errorf("must specify cid of data")
		}

		dataCid, err := cid.Parse(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("parsing data cid: %w", err)
		}

		ds, err := api.ClientDealPieceCID(ctx, dataCid)
		if err != nil {
			return err
		}

		fmt.Printf("Piece CID  : %v\n", ds.PieceCID)
		fmt.Printf("Piece Size  : %v\n", ds.PieceSize)
		fmt.Printf("Payload Size: %v\n", ds.PayloadSize)

		return nil
	},
}

var clientRestartTransfer = &cli.Command{
	Name:  "restart-transfer",
	Usage: "Force restart a stalled data transfer",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "peerid",
			Usage: "narrow to transfer with specific peer",
		},
		&cli.BoolFlag{
			Name:  "initiator",
			Usage: "specify only transfers where peer is/is not initiator",
			Value: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		transferUint, err := strconv.ParseUint(cctx.Args().First(), 10, 64)
		if err != nil {
			return fmt.Errorf("Error reading transfer ID: %w", err)
		}
		transferID := datatransfer.TransferID(transferUint)
		initiator := cctx.Bool("initiator")
		var other peer.ID
		if pidstr := cctx.String("peerid"); pidstr != "" {
			p, err := peer.Decode(pidstr)
			if err != nil {
				return err
			}
			other = p
		} else {
			channels, err := api.ClientListDataTransfers(ctx)
			if err != nil {
				return err
			}
			found := false
			for _, channel := range channels {
				if channel.IsInitiator == initiator && channel.TransferID == transferID {
					other = channel.OtherPeer
					found = true
					break
				}
			}
			if !found {
				return errors.New("unable to find matching data transfer")
			}
		}

		return api.ClientRestartDataTransfer(ctx, transferID, other, initiator)
	},
}

var clientCancelTransfer = &cli.Command{
	Name:  "cancel-transfer",
	Usage: "Force cancel a data transfer",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "peerid",
			Usage: "narrow to transfer with specific peer",
		},
		&cli.BoolFlag{
			Name:  "initiator",
			Usage: "specify only transfers where peer is/is not initiator",
			Value: true,
		},
		&cli.DurationFlag{
			Name:  "cancel-timeout",
			Usage: "time to wait for cancel to be sent to storage provider",
			Value: 5 * time.Second,
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		transferUint, err := strconv.ParseUint(cctx.Args().First(), 10, 64)
		if err != nil {
			return fmt.Errorf("Error reading transfer ID: %w", err)
		}
		transferID := datatransfer.TransferID(transferUint)
		initiator := cctx.Bool("initiator")
		var other peer.ID
		if pidstr := cctx.String("peerid"); pidstr != "" {
			p, err := peer.Decode(pidstr)
			if err != nil {
				return err
			}
			other = p
		} else {
			channels, err := api.ClientListDataTransfers(ctx)
			if err != nil {
				return err
			}
			found := false
			for _, channel := range channels {
				if channel.IsInitiator == initiator && channel.TransferID == transferID {
					other = channel.OtherPeer
					found = true
					break
				}
			}
			if !found {
				return errors.New("unable to find matching data transfer")
			}
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, cctx.Duration("cancel-timeout"))
		defer cancel()
		return api.ClientCancelDataTransfer(timeoutCtx, transferID, other, initiator)
	},
}

var clientCancelAllTransfer = &cli.Command{
	Name:  "cancel-all-transfer",
	Usage: "Force cancel all data transfer",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "force",
			Usage: "force cancel all data transfers",
			Value: false,
		},
		&cli.DurationFlag{
			Name:  "cancel-timeout",
			Usage: "time to wait for cancel to be sent to storage provider",
			Value: 5 * time.Second,
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Bool("force") {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		channels, err := api.ClientListDataTransfers(ctx)
		if err != nil {
			return err
		}

		var cancelChannels []lapi.DataTransferChannel
		for _, channel := range channels {
			if channel.Status == datatransfer.Completed {
				continue
			}
			if channel.Status == datatransfer.Failed || channel.Status == datatransfer.Cancelled {
				continue
			}
			cancelChannels = append(cancelChannels, channel)
		}
		fmt.Printf("cancel data-transfer need cancel channels:%d\n", len(cancelChannels))

		for _, channel := range cancelChannels {
			timeoutCtx, cancel := context.WithTimeout(ctx, cctx.Duration("cancel-timeout"))
			defer cancel()
			if err := api.ClientCancelDataTransfer(timeoutCtx, channel.TransferID, channel.OtherPeer, channel.IsInitiator); err != nil {
				continue
			}
			fmt.Printf("cancel data-transfer:%v, %v, %s\n", channel.TransferID, channel.OtherPeer, datatransfer.Statuses[channel.Status])
		}
		return nil
	},
}

var clientListTransfers = &cli.Command{
	Name:  "list-transfers",
	Usage: "List ongoing data transfers for deals",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "print verbose transfer details",
		},
		&cli.BoolFlag{
			Name:  "color",
			Usage: "use color in display output",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "completed",
			Usage: "show completed data transfers",
		},
		&cli.BoolFlag{
			Name:  "watch",
			Usage: "watch deal updates in real-time, rather than a one time list",
		},
		&cli.BoolFlag{
			Name:  "show-failed",
			Usage: "show failed/cancelled transfers",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		channels, err := api.ClientListDataTransfers(ctx)
		if err != nil {
			return err
		}

		verbose := cctx.Bool("verbose")
		completed := cctx.Bool("completed")
		color := cctx.Bool("color")
		watch := cctx.Bool("watch")
		showFailed := cctx.Bool("show-failed")
		if watch {
			channelUpdates, err := api.ClientDataTransferUpdates(ctx)
			if err != nil {
				return err
			}

			for {
				tm.Clear() // Clear current screen

				tm.MoveCursor(1, 1)

				OutputDataTransferChannels(tm.Screen, channels, verbose, completed, color, showFailed)

				tm.Flush()

				select {
				case <-ctx.Done():
					return nil
				case channelUpdate := <-channelUpdates:
					var found bool
					for i, existing := range channels {
						if existing.TransferID == channelUpdate.TransferID &&
							existing.OtherPeer == channelUpdate.OtherPeer &&
							existing.IsSender == channelUpdate.IsSender &&
							existing.IsInitiator == channelUpdate.IsInitiator {
							channels[i] = channelUpdate
							found = true
							break
						}
					}
					if !found {
						channels = append(channels, channelUpdate)
					}
				}
			}
		}
		OutputDataTransferChannels(os.Stdout, channels, verbose, completed, color, showFailed)
		return nil
	},
}

// OutputDataTransferChannels generates table output for a list of channels
func OutputDataTransferChannels(out io.Writer, channels []lapi.DataTransferChannel, verbose, completed, color, showFailed bool) {
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].TransferID < channels[j].TransferID
	})

	var receivingChannels, sendingChannels []lapi.DataTransferChannel
	for _, channel := range channels {
		if !completed && channel.Status == datatransfer.Completed {
			continue
		}
		if !showFailed && (channel.Status == datatransfer.Failed || channel.Status == datatransfer.Cancelled) {
			continue
		}
		if channel.IsSender {
			sendingChannels = append(sendingChannels, channel)
		} else {
			receivingChannels = append(receivingChannels, channel)
		}
	}

	fmt.Fprintf(out, "Sending Channels\n\n")
	w := tablewriter.New(tablewriter.Col("ID"),
		tablewriter.Col("Status"),
		tablewriter.Col("Sending To"),
		tablewriter.Col("Root Cid"),
		tablewriter.Col("Initiated?"),
		tablewriter.Col("Transferred"),
		tablewriter.Col("Voucher"),
		tablewriter.NewLineCol("Message"))
	for _, channel := range sendingChannels {
		w.Write(toChannelOutput(color, "Sending To", channel, verbose))
	}
	w.Flush(out) //nolint:errcheck

	fmt.Fprintf(out, "\nReceiving Channels\n\n")
	w = tablewriter.New(tablewriter.Col("ID"),
		tablewriter.Col("Status"),
		tablewriter.Col("Receiving From"),
		tablewriter.Col("Root Cid"),
		tablewriter.Col("Initiated?"),
		tablewriter.Col("Transferred"),
		tablewriter.Col("Voucher"),
		tablewriter.NewLineCol("Message"))
	for _, channel := range receivingChannels {
		w.Write(toChannelOutput(color, "Receiving From", channel, verbose))
	}
	w.Flush(out) //nolint:errcheck
}

func channelStatusString(useColor bool, status datatransfer.Status) string {
	s := datatransfer.Statuses[status]
	if !useColor {
		return s
	}

	switch status {
	case datatransfer.Failed, datatransfer.Cancelled:
		return color.RedString(s)
	case datatransfer.Completed:
		return color.GreenString(s)
	default:
		return s
	}
}

func toChannelOutput(useColor bool, otherPartyColumn string, channel lapi.DataTransferChannel, verbose bool) map[string]interface{} {
	rootCid := channel.BaseCID.String()
	otherParty := channel.OtherPeer.String()
	if !verbose {
		rootCid = ellipsis(rootCid, 8)
		otherParty = ellipsis(otherParty, 8)
	}

	initiated := "N"
	if channel.IsInitiator {
		initiated = "Y"
	}

	voucher := channel.Voucher
	if len(voucher) > 40 && !verbose {
		voucher = ellipsis(voucher, 37)
	}

	return map[string]interface{}{
		"ID":             channel.TransferID,
		"Status":         channelStatusString(useColor, channel.Status),
		otherPartyColumn: otherParty,
		"Root Cid":       rootCid,
		"Initiated?":     initiated,
		"Transferred":    units.BytesSize(float64(channel.Transferred)),
		"Voucher":        voucher,
		"Message":        channel.Message,
	}
}

func ellipsis(s string, length int) string {
	if length > 0 && len(s) > length {
		return "..." + s[len(s)-length:]
	}
	return s
}
