package cli

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin/vesting"

	"github.com/EpiK-Protocol/go-epik/chain/actors"
	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin/miner"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/lib/tablewriter"
)

var walletCmd = &cli.Command{
	Name:  "wallet",
	Usage: "Manage wallet",
	Subcommands: []*cli.Command{
		walletNew,
		walletList,
		walletBalance,
		walletExport,
		walletImport,
		walletGetDefault,
		walletSetDefault,
		walletSign,
		walletVerify,
		walletDelete,
		// walletMarket,
		walletCoinbase,
	},
}

var walletNew = &cli.Command{
	Name:      "new",
	Usage:     "Generate a new key of the given type",
	ArgsUsage: "[bls|secp256k1 (default secp256k1)]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		t := cctx.Args().First()
		if t == "" {
			t = "secp256k1"
		}

		nk, err := api.WalletNew(ctx, types.KeyType(t))
		if err != nil {
			return err
		}

		fmt.Println(nk.String())

		return nil
	},
}

var walletList = &cli.Command{
	Name:  "list",
	Usage: "List wallet address",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "addr-only",
			Usage:   "Only print addresses",
			Aliases: []string{"a"},
		},
		&cli.BoolFlag{
			Name:    "id",
			Usage:   "Output ID addresses",
			Aliases: []string{"i"},
		},
		// &cli.BoolFlag{
		// 	Name:    "market",
		// 	Usage:   "Output market balances",
		// 	Aliases: []string{"m"},
		// },
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		addrs, err := api.WalletList(ctx)
		if err != nil {
			return err
		}

		// Assume an error means no default key is set
		def, _ := api.WalletDefaultAddress(ctx)

		tw := tablewriter.New(
			tablewriter.Col("Address"),
			tablewriter.Col("ID"),
			tablewriter.Col("Balance"),
			tablewriter.Col("Market(Avail)"),
			tablewriter.Col("Market(Locked)"),
			tablewriter.Col("Nonce"),
			tablewriter.Col("Default"),
			tablewriter.NewLineCol("Error"))

		for _, addr := range addrs {
			if cctx.Bool("addr-only") {
				fmt.Println(addr.String())
			} else {
				a, err := api.StateGetActor(ctx, addr, types.EmptyTSK)
				if err != nil {
					if !strings.Contains(err.Error(), "actor not found") {
						tw.Write(map[string]interface{}{
							"Address": addr,
							"Error":   err,
						})
						continue
					}

					a = &types.Actor{
						Balance: big.Zero(),
					}
				}

				row := map[string]interface{}{
					"Address": addr,
					"Balance": types.EPK(a.Balance),
					"Nonce":   a.Nonce,
				}
				if addr == def {
					row["Default"] = "X"
				}

				if cctx.Bool("id") {
					id, err := api.StateLookupID(ctx, addr, types.EmptyTSK)
					if err != nil {
						row["ID"] = "n/a"
					} else {
						row["ID"] = id
					}
				}

				// if cctx.Bool("market") {
				// 	mbal, err := api.StateMarketBalance(ctx, addr, types.EmptyTSK)
				// 	if err == nil {
				// 		row["Market(Avail)"] = types.EPK(types.BigSub(mbal.Escrow, mbal.Locked))
				// 		row["Market(Locked)"] = types.EPK(mbal.Locked)
				// 	}
				// }

				tw.Write(row)
			}
		}

		if !cctx.Bool("addr-only") {
			return tw.Flush(os.Stdout)
		}

		return nil
	},
}

var walletBalance = &cli.Command{
	Name:      "balance",
	Usage:     "Get account balance",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var addr address.Address
		if cctx.Args().First() != "" {
			addr, err = address.NewFromString(cctx.Args().First())
		} else {
			addr, err = api.WalletDefaultAddress(ctx)
		}
		if err != nil {
			return err
		}

		balance, err := api.WalletBalance(ctx, addr)
		if err != nil {
			return err
		}

		if balance.Equals(types.NewInt(0)) {
			fmt.Printf("%s (warning: may display 0 if chain sync in progress)\n", types.EPK(balance))
		} else {
			fmt.Printf("%s\n", types.EPK(balance))
		}

		return nil
	},
}

var walletGetDefault = &cli.Command{
	Name:  "default",
	Usage: "Get default wallet address",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		addr, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			return err
		}

		fmt.Printf("%s\n", addr.String())
		return nil
	},
}

var walletSetDefault = &cli.Command{
	Name:      "set-default",
	Usage:     "Set default wallet address",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() {
			return fmt.Errorf("must pass address to set as default")
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		return api.WalletSetDefault(ctx, addr)
	},
}

var walletExport = &cli.Command{
	Name:      "export",
	Usage:     "export keys",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() {
			return fmt.Errorf("must specify key to export")
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		ki, err := api.WalletExport(ctx, addr)
		if err != nil {
			return err
		}

		b, err := json.Marshal(ki)
		if err != nil {
			return err
		}

		fmt.Println(hex.EncodeToString(b))
		return nil
	},
}

var walletImport = &cli.Command{
	Name:      "import",
	Usage:     "import keys",
	ArgsUsage: "[<path> (optional, will read from stdin if omitted)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Usage: "specify input format for key",
			Value: "hex-epik",
		},
		&cli.BoolFlag{
			Name:  "as-default",
			Usage: "import the given key as your new default key",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		var inpdata []byte
		if !cctx.Args().Present() || cctx.Args().First() == "-" {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Enter private key: ")
			indata, err := reader.ReadBytes('\n')
			if err != nil {
				return err
			}
			inpdata = indata

		} else {
			fdata, err := ioutil.ReadFile(cctx.Args().First())
			if err != nil {
				return err
			}
			inpdata = fdata
		}

		var ki types.KeyInfo
		switch cctx.String("format") {
		case "hex-epik":
			data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
			if err != nil {
				return err
			}

			if err := json.Unmarshal(data, &ki); err != nil {
				return err
			}
		case "json-epik":
			if err := json.Unmarshal(inpdata, &ki); err != nil {
				return err
			}
		case "gfc-json":
			var f struct {
				KeyInfo []struct {
					PrivateKey []byte
					SigType    int
				}
			}
			if err := json.Unmarshal(inpdata, &f); err != nil {
				return xerrors.Errorf("failed to parse go-epik key: %s", err)
			}

			gk := f.KeyInfo[0]
			ki.PrivateKey = gk.PrivateKey
			switch gk.SigType {
			case 1:
				ki.Type = types.KTSecp256k1
			case 2:
				ki.Type = types.KTBLS
			default:
				return fmt.Errorf("unrecognized key type: %d", gk.SigType)
			}
		default:
			return fmt.Errorf("unrecognized format: %s", cctx.String("format"))
		}

		addr, err := api.WalletImport(ctx, &ki)
		if err != nil {
			return err
		}

		if cctx.Bool("as-default") {
			if err := api.WalletSetDefault(ctx, addr); err != nil {
				return fmt.Errorf("failed to set default key: %w", err)
			}
		}

		fmt.Printf("imported key %s successfully!\n", addr)
		return nil
	},
}

var walletSign = &cli.Command{
	Name:      "sign",
	Usage:     "sign a message",
	ArgsUsage: "<signing address> <hexMessage>",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() || cctx.NArg() != 2 {
			return fmt.Errorf("must specify signing address and message to sign")
		}

		addr, err := address.NewFromString(cctx.Args().First())

		if err != nil {
			return err
		}

		msg, err := hex.DecodeString(cctx.Args().Get(1))

		if err != nil {
			return err
		}

		sig, err := api.WalletSign(ctx, addr, msg)

		if err != nil {
			return err
		}

		sigBytes := append([]byte{byte(sig.Type)}, sig.Data...)

		fmt.Println(hex.EncodeToString(sigBytes))
		return nil
	},
}

var walletVerify = &cli.Command{
	Name:      "verify",
	Usage:     "verify the signature of a message",
	ArgsUsage: "<signing address> <hexMessage> <signature>",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() || cctx.NArg() != 3 {
			return fmt.Errorf("must specify signing address, message, and signature to verify")
		}

		addr, err := address.NewFromString(cctx.Args().First())

		if err != nil {
			return err
		}

		msg, err := hex.DecodeString(cctx.Args().Get(1))

		if err != nil {
			return err
		}

		sigBytes, err := hex.DecodeString(cctx.Args().Get(2))

		if err != nil {
			return err
		}

		var sig crypto.Signature
		if err := sig.UnmarshalBinary(sigBytes); err != nil {
			return err
		}

		ok, err := api.WalletVerify(ctx, addr, msg, &sig)
		if err != nil {
			return err
		}
		if ok {
			fmt.Println("valid")
			return nil
		}
		fmt.Println("invalid")
		return NewCliError("CLI Verify called with invalid signature")
	},
}

var walletDelete = &cli.Command{
	Name:      "delete",
	Usage:     "Delete an account from the wallet",
	ArgsUsage: "<address> ",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := ReqContext(cctx)

		if !cctx.Args().Present() || cctx.NArg() != 1 {
			return fmt.Errorf("must specify address to delete")
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		return api.WalletDelete(ctx, addr)
	},
}

// var walletMarket = &cli.Command{
// 	Name:  "market",
// 	Usage: "Interact with market balances",
// 	Subcommands: []*cli.Command{
// 		walletMarketWithdraw,
// 		walletMarketAdd,
// 	},
// }

// var walletMarketWithdraw = &cli.Command{
// 	Name:      "withdraw",
// 	Usage:     "Withdraw funds from the Storage Market Actor",
// 	ArgsUsage: "[amount (EPK) optional, otherwise will withdraw max available]",
// 	Flags: []cli.Flag{
// 		&cli.StringFlag{
// 			Name:    "wallet",
// 			Usage:   "Specify address to withdraw funds to, otherwise it will use the default wallet address",
// 			Aliases: []string{"w"},
// 		},
// 		&cli.StringFlag{
// 			Name:    "address",
// 			Usage:   "Market address to withdraw from (account or miner actor address, defaults to --wallet address)",
// 			Aliases: []string{"a"},
// 		},
// 	},
// 	Action: func(cctx *cli.Context) error {
// 		api, closer, err := GetFullNodeAPI(cctx)
// 		if err != nil {
// 			return xerrors.Errorf("getting node API: %w", err)
// 		}
// 		defer closer()
// 		ctx := ReqContext(cctx)

// 		var wallet address.Address
// 		if cctx.String("wallet") != "" {
// 			wallet, err = address.NewFromString(cctx.String("wallet"))
// 			if err != nil {
// 				return xerrors.Errorf("parsing from address: %w", err)
// 			}
// 		} else {
// 			wallet, err = api.WalletDefaultAddress(ctx)
// 			if err != nil {
// 				return xerrors.Errorf("getting default wallet address: %w", err)
// 			}
// 		}

// 		addr := wallet
// 		if cctx.String("address") != "" {
// 			addr, err = address.NewFromString(cctx.String("address"))
// 			if err != nil {
// 				return xerrors.Errorf("parsing market address: %w", err)
// 			}
// 		}

// 		// Work out if there are enough unreserved, unlocked funds to withdraw
// 		bal, err := api.StateMarketBalance(ctx, addr, types.EmptyTSK)
// 		if err != nil {
// 			return xerrors.Errorf("getting market balance for address %s: %w", addr.String(), err)
// 		}

// 		reserved, err := api.MarketGetReserved(ctx, addr)
// 		if err != nil {
// 			return xerrors.Errorf("getting market reserved amount for address %s: %w", addr.String(), err)
// 		}

// 		avail := big.Subtract(big.Subtract(bal.Escrow, bal.Locked), reserved)

// 		notEnoughErr := func(msg string) error {
// 			return xerrors.Errorf("%s; "+
// 				"available (%s) = escrow (%s) - locked (%s) - reserved (%s)",
// 				msg, types.EPK(avail), types.EPK(bal.Escrow), types.EPK(bal.Locked), types.EPK(reserved))
// 		}

// 		if avail.IsZero() || avail.LessThan(big.Zero()) {
// 			avail = big.Zero()
// 			return notEnoughErr("no funds available to withdraw")
// 		}

// 		// Default to withdrawing all available funds
// 		amt := avail

// 		// If there was an amount argument, only withdraw that amount
// 		if cctx.Args().Present() {
// 			f, err := types.ParseEPK(cctx.Args().First())
// 			if err != nil {
// 				return xerrors.Errorf("parsing 'amount' argument: %w", err)
// 			}

// 			amt = abi.TokenAmount(f)
// 		}

// 		// Check the amount is positive
// 		if amt.IsZero() || amt.LessThan(big.Zero()) {
// 			return xerrors.Errorf("amount must be > 0")
// 		}

// 		// Check there are enough available funds
// 		if amt.GreaterThan(avail) {
// 			msg := fmt.Sprintf("can't withdraw more funds than available; requested: %s", types.EPK(amt))
// 			return notEnoughErr(msg)
// 		}

// 		fmt.Printf("Submitting WithdrawBalance message for amount %s for address %s\n", types.EPK(amt), wallet.String())
// 		smsg, err := api.MarketWithdraw(ctx, wallet, addr, amt)
// 		if err != nil {
// 			return xerrors.Errorf("fund manager withdraw error: %w", err)
// 		}

// 		fmt.Printf("WithdrawBalance message cid: %s\n", smsg)

// 		return nil
// 	},
// }

// var walletMarketAdd = &cli.Command{
// 	Name:      "add",
// 	Usage:     "Add funds to the Storage Market Actor",
// 	ArgsUsage: "<amount>",
// 	Flags: []cli.Flag{
// 		&cli.StringFlag{
// 			Name:    "from",
// 			Usage:   "Specify address to move funds from, otherwise it will use the default wallet address",
// 			Aliases: []string{"f"},
// 		},
// 		&cli.StringFlag{
// 			Name:    "address",
// 			Usage:   "Market address to move funds to (account or miner actor address, defaults to --from address)",
// 			Aliases: []string{"a"},
// 		},
// 	},
// 	Action: func(cctx *cli.Context) error {
// 		api, closer, err := GetFullNodeAPI(cctx)
// 		if err != nil {
// 			return xerrors.Errorf("getting node API: %w", err)
// 		}
// 		defer closer()
// 		ctx := ReqContext(cctx)

// 		// Get amount param
// 		if !cctx.Args().Present() {
// 			return fmt.Errorf("must pass amount to add")
// 		}
// 		f, err := types.ParseEPK(cctx.Args().First())
// 		if err != nil {
// 			return xerrors.Errorf("parsing 'amount' argument: %w", err)
// 		}

// 		amt := abi.TokenAmount(f)

// 		// Get from param
// 		var from address.Address
// 		if cctx.String("from") != "" {
// 			from, err = address.NewFromString(cctx.String("from"))
// 			if err != nil {
// 				return xerrors.Errorf("parsing from address: %w", err)
// 			}
// 		} else {
// 			from, err = api.WalletDefaultAddress(ctx)
// 			if err != nil {
// 				return xerrors.Errorf("getting default wallet address: %w", err)
// 			}
// 		}

// 		// Get address param
// 		addr := from
// 		if cctx.String("address") != "" {
// 			addr, err = address.NewFromString(cctx.String("address"))
// 			if err != nil {
// 				return xerrors.Errorf("parsing market address: %w", err)
// 			}
// 		}

// 		// Add balance to market actor
// 		fmt.Printf("Submitting Add Balance message for amount %s for address %s\n", types.EPK(amt), addr)
// 		smsg, err := api.MarketAddBalance(ctx, from, addr, amt)
// 		if err != nil {
// 			return xerrors.Errorf("add balance error: %w", err)
// 		}

// 		fmt.Printf("AddBalance message cid: %s\n", smsg)

// 		return nil
// 	},
// }

var walletCoinbase = &cli.Command{
	Name:  "coinbase",
	Usage: "Interact with coinbase balances",
	Subcommands: []*cli.Command{
		walletCoinbaseWithdraw,
		walletCoinbaseInfo,
		walletCoinbaseChange,
	},
}

var walletCoinbaseWithdraw = &cli.Command{
	Name:      "withdraw",
	Usage:     "Withdraw funds from the coinbase",
	ArgsUsage: "[amount (EPK) optional, otherwise will withdraw max available]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Usage:   "Specify coinbase address to withdraw from, otherwise it will use the default wallet address",
			Aliases: []string{"a"},
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting node API: %w", err)
		}
		defer closer()
		ctx := ReqContext(cctx)

		var coinbase address.Address
		if cctx.String("address") != "" {
			coinbase, err = address.NewFromString(cctx.String("address"))
			if err != nil {
				return xerrors.Errorf("parsing coinbase: %w", err)
			}
		} else {
			coinbase, err = api.WalletDefaultAddress(ctx)
			if err != nil {
				return xerrors.Errorf("getting default wallet address: %w", err)
			}
		}

		amount := abi.NewTokenAmount(0)
		if cctx.Args().Present() {
			f, err := types.ParseEPK(cctx.Args().First())
			if err != nil {
				return xerrors.Errorf("parsing 'amount' argument: %w", err)
			}
			amount = abi.TokenAmount(f)
		} else {
			ida, err := api.StateLookupID(ctx, coinbase, types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("looking up id address: %w", err)
			}

			ci, err := api.StateCoinbase(ctx, ida, types.EmptyTSK)
			if err != nil {
				return err
			}
			amount = ci.Vested
		}

		params, err := actors.SerializeParams(&vesting.WithdrawBalanceParams{
			AmountRequested: amount,
		})
		if err != nil {
			return err
		}

		smsg, err := api.MpoolPushMessage(ctx, &types.Message{
			To:     builtin.VestingActorAddr,
			From:   coinbase,
			Value:  types.NewInt(0),
			Method: builtin.MethodsVesting.WithdrawBalance,
			Params: params,
		}, nil)
		if err != nil {
			return err
		}

		fmt.Printf("WithdrawBalance message cid: %s\n", smsg.Cid())

		return nil
	},
}

var walletCoinbaseInfo = &cli.Command{
	Name:      "info",
	Usage:     "Show coinbase info",
	ArgsUsage: "[coinbase optional, default the wallet address]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "tipset",
			Usage: "specify tipset to call method on (pass comma separated array of cids)",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting node API: %w", err)
		}
		defer closer()
		ctx := ReqContext(cctx)

		var coinbase address.Address
		if !cctx.Args().Present() {
			coinbase, err = api.WalletDefaultAddress(ctx)
			if err != nil {
				return xerrors.Errorf("getting default wallet address: %w", err)
			}
		} else {
			coinbase, err = address.NewFromString(cctx.Args().First())
			if err != nil {
				return xerrors.Errorf("parsing coinbase: %w", err)
			}
		}

		ts, err := LoadTipSet(ctx, cctx, api)
		if err != nil {
			return err
		}

		ida, err := api.StateLookupID(ctx, coinbase, ts.Key())
		if err != nil {
			return xerrors.Errorf("looking up id address: %w", err)
		}

		ci, err := api.StateCoinbase(ctx, ida, ts.Key())
		if err != nil {
			return err
		}

		fmt.Printf("Coinbase Balance:   %s\n", types.EPK(ci.Total))
		fmt.Printf("\t Vesting:   %s\n", types.EPK(ci.Vesting))
		fmt.Printf("\t Available: %s\n", types.EPK(ci.Vested))

		return nil
	},
}

var walletCoinbaseChange = &cli.Command{
	Name:      "change",
	Usage:     "change miner coinbase",
	ArgsUsage: "<miner> <coinbase>",
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting node API: %w", err)
		}
		defer closer()
		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return xerrors.Errorf("parsing miner: %w", err)
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		coinbase, err := address.NewFromString(cctx.Args().Get(1))
		if err != nil {
			return xerrors.Errorf("parsing coinbase: %w", err)
		}

		sp, err := actors.SerializeParams(&coinbase)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		smsg, err := api.MpoolPushMessage(ctx, &types.Message{
			From:   mi.Owner,
			To:     maddr,
			Method: miner.Methods.ChangeCoinbase,
			Value:  big.Zero(),
			Params: sp,
		}, nil)
		if err != nil {
			return xerrors.Errorf("mpool push: %w", err)
		}

		fmt.Println("Send Message CID:", smsg.Cid())

		return nil
	},
}
