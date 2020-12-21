package genesis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/EpiK-Protocol/go-epik/chain/actors/builtin"
	"github.com/EpiK-Protocol/go-epik/journal"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	builtin2 "github.com/filecoin-project/specs-actors/v2/actors/builtin"
	account2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/account"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin/govern"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin/knowledge"
	multisig2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/multisig"
	adt2 "github.com/filecoin-project/specs-actors/v2/actors/util/adt"

	"github.com/EpiK-Protocol/go-epik/build"
	"github.com/EpiK-Protocol/go-epik/chain/state"
	"github.com/EpiK-Protocol/go-epik/chain/store"
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/chain/vm"
	"github.com/EpiK-Protocol/go-epik/genesis"
	bstore "github.com/EpiK-Protocol/go-epik/lib/blockstore"
)

const AccountStart = 100
const MinerStart = 1000
const MaxAccounts = MinerStart - AccountStart

var log = logging.Logger("genesis")

type GenesisBootstrap struct {
	Genesis *types.BlockHeader
}

/*
From a list of parameters, create a genesis block / initial state

The process:
- Bootstrap state (MakeInitialStateTree)
  - Create empty state
  - Create system actor
  - Make init actor
    - Create accounts mappings
    - Set NextID to MinerStart
  - Setup Reward (0.7B epk)
  - Setup Cron
  - Create empty power actor
  - Create empty market actor
  - Create empty govern actor
  - Setup burnt fund address
  - Setup expert funds actor
  - Setup retrieval funds actor
  - Setup vote funds actor
  - Setup knowledge funds actor
  - Initialize account / msig balances
- Instantiate early vm with genesis syscalls
  - Create miners
    - Each:
      - power.CreateMiner, set msg value to PowerBalance
      - market.AddFunds with correct value
      - market.PublishDeals for related sectors
    - Set network power in the power actor to what we'll have after genesis creation
	- Recreate reward actor state with the right power
    - For each precommitted sector
      - Get deal weight
      - Calculate QA Power
      - Remove fake power from the power actor
      - Calculate pledge
      - Precommit
      - Confirm valid

Data Types:

PreSeal :{
  CommR    CID
  CommD    CID
  SectorID SectorNumber
  Deal     market.DealProposal # Start at 0, self-deal!
}

Genesis: {
	Accounts: [ # non-miner, non-singleton actors, max len = MaxAccounts
		{
			Type: "account" / "multisig",
			Value: "attoepk",
			[Meta: {msig settings, account key..}]
		},...
	],
	Miners: [
		{
			Owner, Worker Addr # ID
			MarketBalance, PowerBalance TokenAmount
			SectorSize uint64
			PreSeals []PreSeal
		},...
	],
}

*/

func MakeInitialStateTree(ctx context.Context, bs bstore.Blockstore, template genesis.Template) (*state.StateTree, map[address.Address]address.Address, error) {
	// Create empty state tree

	cst := cbor.NewCborStore(bs)
	_, err := cst.Put(context.TODO(), []struct{}{})
	if err != nil {
		return nil, nil, xerrors.Errorf("putting empty object: %w", err)
	}

	state, err := state.NewStateTree(cst, types.StateTreeVersion1)
	if err != nil {
		return nil, nil, xerrors.Errorf("making new state tree: %w", err)
	}

	// Create system actor

	sysact, err := SetupSystemActor(bs)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}
	if err := state.SetActor(builtin2.SystemActorAddr, sysact); err != nil {
		return nil, nil, xerrors.Errorf("set init actor: %w", err)
	}

	// Create init actor

	idStart, initact, keyIDs, err := SetupInitActor(bs, template.NetworkName,
		append(template.Accounts, template.FirstGovernorAccountActor, template.FoundationAccountActor, template.FundraisingAccountActor, template.TeamAccountActor))
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}
	if err := state.SetActor(builtin2.InitActorAddr, initact); err != nil {
		return nil, nil, xerrors.Errorf("set init actor: %w", err)
	}

	// Setup reward
	// RewardActor's state is overrwritten by SetupStorageMiners
	rewact, err := SetupRewardActor(bs, big.Zero())
	if err != nil {
		return nil, nil, xerrors.Errorf("setup init actor: %w", err)
	}

	err = state.SetActor(builtin2.RewardActorAddr, rewact)
	if err != nil {
		return nil, nil, xerrors.Errorf("set network account actor: %w", err)
	}

	// Setup cron
	cronact, err := SetupCronActor(bs)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup cron actor: %w", err)
	}
	if err := state.SetActor(builtin2.CronActorAddr, cronact); err != nil {
		return nil, nil, xerrors.Errorf("set cron actor: %w", err)
	}

	// Create empty power actor
	spact, err := SetupStoragePowerActor(bs)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage power actor: %w", err)
	}
	if err := state.SetActor(builtin2.StoragePowerActorAddr, spact); err != nil {
		return nil, nil, xerrors.Errorf("set storage market actor: %w", err)
	}

	// Create empty market actor
	marketact, err := SetupStorageMarketActor(bs)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup storage market actor: %w", err)
	}
	if err := state.SetActor(builtin2.StorageMarketActorAddr, marketact); err != nil {
		return nil, nil, xerrors.Errorf("set market actor: %w", err)
	}

	// Create empty govern actor
	governact, err := SetupGovernActor(bs, builtin.FoundationAddress)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup govern actor: %w", err)
	}
	if err := state.SetActor(builtin2.GovernActorAddr, governact); err != nil {
		return nil, nil, xerrors.Errorf("set govern actor: %w", err)
	}

	// Setup burnt-funds
	burntRoot, err := cst.Put(ctx, &account2.State{
		Address: builtin2.BurntFundsActorAddr,
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to setup burnt funds actor state: %w", err)
	}
	err = state.SetActor(builtin2.BurntFundsActorAddr, &types.Actor{
		Code:    builtin2.AccountActorCodeID,
		Balance: types.NewInt(0),
		Head:    burntRoot,
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("set burnt funds account actor: %w", err)
	}

	// Setup expert-funds
	expertRoot, err := cst.Put(ctx, &account2.State{
		Address: builtin2.ExpertFundsActorAddr,
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to setup burnt funds actor state: %w", err)
	}
	err = state.SetActor(builtin2.ExpertFundsActorAddr, &types.Actor{
		Code:    builtin2.AccountActorCodeID,
		Balance: types.NewInt(0),
		Head:    expertRoot,
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("set expert funds account actor: %w", err)
	}

	// Setup retrieve-funds
	retrieveRoot, err := cst.Put(ctx, &account2.State{
		Address: builtin2.RetrieveFundsActorAddr,
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to setup burnt funds actor state: %w", err)
	}
	err = state.SetActor(builtin2.RetrieveFundsActorAddr, &types.Actor{
		Code:    builtin2.AccountActorCodeID,
		Balance: types.NewInt(0),
		Head:    retrieveRoot,
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("set retrieve funds account actor: %w", err)
	}

	// Create empty vote actor
	voteact, err := SetupVoteActor(bs)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup vote actor: %w", err)
	}
	if err := state.SetActor(builtin2.VoteFundsActorAddr, voteact); err != nil {
		return nil, nil, xerrors.Errorf("set vote actor: %w", err)
	}

	// Create empty knowledge actor
	knowact, err := SetupKnowledgeActor(bs, builtin.FirstGovernorAddress)
	if err != nil {
		return nil, nil, xerrors.Errorf("setup knowledge actor: %w", err)
	}
	if err := state.SetActor(builtin2.KnowledgeFundsActorAddr, knowact); err != nil {
		return nil, nil, xerrors.Errorf("set knowledge actor: %w", err)
	}

	// Create accounts
	for _, info := range template.Accounts {

		switch info.Type {
		case genesis.TAccount:
			if err := createAccountActor(ctx, cst, state, info, keyIDs); err != nil {
				return nil, nil, xerrors.Errorf("failed to create account actor: %w", err)
			}

		case genesis.TMultisig:

			ida, err := address.NewIDAddress(uint64(idStart))
			if err != nil {
				return nil, nil, err
			}
			idStart++

			if err := createMultisigAccount(ctx, bs, cst, state, ida, info, keyIDs); err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, xerrors.New("unsupported account type")
		}

	}

	// vregroot, err := address.NewIDAddress(80)
	// if err != nil {
	// 	return nil, nil, err
	// }

	// if err = createMultisigAccount(ctx, bs, cst, state, vregroot, template.VerifregRootKey, keyIDs); err != nil {
	// 	return nil, nil, xerrors.Errorf("failed to set up verified registry signer: %w", err)
	// }

	// // Setup the first verifier as ID-address 81
	// // TODO: remove this
	// skBytes, err := sigs.Generate(crypto.SigTypeBLS)
	// if err != nil {
	// 	return nil, nil, xerrors.Errorf("creating random verifier secret key: %w", err)
	// }

	// verifierPk, err := sigs.ToPublic(crypto.SigTypeBLS, skBytes)
	// if err != nil {
	// 	return nil, nil, xerrors.Errorf("creating random verifier public key: %w", err)
	// }

	// verifierAd, err := address.NewBLSAddress(verifierPk)
	// if err != nil {
	// 	return nil, nil, xerrors.Errorf("creating random verifier address: %w", err)
	// }

	// verifierId, err := address.NewIDAddress(81)
	// if err != nil {
	// 	return nil, nil, err
	// }

	// verifierState, err := cst.Put(ctx, &account2.State{Address: verifierAd})
	// if err != nil {
	// 	return nil, nil, err
	// }

	// err = state.SetActor(verifierId, &types.Actor{
	// 	Code:    builtin2.AccountActorCodeID,
	// 	Balance: types.NewInt(0),
	// 	Head:    verifierState,
	// })
	// if err != nil {
	// 	return nil, nil, xerrors.Errorf("setting account from actmap: %w", err)
	// }

	if err := createMultisigAccount(ctx, bs, cst, state, builtin.FirstGovernorAddress, template.FirstGovernorAccountActor, keyIDs); err != nil {
		return nil, nil, xerrors.Errorf("failed to set up govern account: %w", err)
	}
	if err := createMultisigAccount(ctx, bs, cst, state, builtin.FundraisingAddress, template.FundraisingAccountActor, keyIDs); err != nil {
		return nil, nil, xerrors.Errorf("failed to set up fundraising account: %w", err)
	}
	if err := createMultisigAccount(ctx, bs, cst, state, builtin.TeamAddress, template.TeamAccountActor, keyIDs); err != nil {
		return nil, nil, xerrors.Errorf("failed to set up team account: %w", err)
	}

	totalEpkAllocated := big.Zero()

	// flush as ForEach works on the HAMT
	if _, err := state.Flush(ctx); err != nil {
		return nil, nil, err
	}
	err = state.ForEach(func(addr address.Address, act *types.Actor) error {
		totalEpkAllocated = big.Add(totalEpkAllocated, act.Balance)
		return nil
	})
	if err != nil {
		return nil, nil, xerrors.Errorf("summing account balances in state tree: %w", err)
	}

	totalEpk := big.Mul(big.NewInt(int64(build.EpkBase)), big.NewInt(int64(build.EpkPrecision)))
	template.FoundationAccountActor.Balance = big.Sub(totalEpk, totalEpkAllocated)
	if template.FoundationAccountActor.Balance.Sign() < 0 {
		return nil, nil, xerrors.Errorf("somehow overallocated epk (allocated = %s)", types.EPK(totalEpkAllocated))
	}

	if err := createMultisigAccount(ctx, bs, cst, state, builtin.FoundationAddress, template.FoundationAccountActor, keyIDs); err != nil {
		return nil, nil, xerrors.Errorf("failed to set up foundation account: %w", err)
	}

	// template.RemainderAccount.Balance = remainingEpk

	// if err := createMultisigAccount(ctx, bs, cst, state, builtin.ReserveAddress, template.RemainderAccount, keyIDs); err != nil {
	// 	return nil, nil, xerrors.Errorf("failed to set up remainder account: %w", err)
	// }

	return state, keyIDs, nil
}

func createAccountActor(ctx context.Context, cst cbor.IpldStore, state *state.StateTree, info genesis.Actor, keyIDs map[address.Address]address.Address) error {
	var ainfo genesis.AccountMeta
	if err := json.Unmarshal(info.Meta, &ainfo); err != nil {
		return xerrors.Errorf("unmarshaling account meta: %w", err)
	}
	st, err := cst.Put(ctx, &account2.State{Address: ainfo.Owner})
	if err != nil {
		return err
	}

	ida, ok := keyIDs[ainfo.Owner]
	if !ok {
		return fmt.Errorf("no registered ID for account actor: %s", ainfo.Owner)
	}

	err = state.SetActor(ida, &types.Actor{
		Code:    builtin2.AccountActorCodeID,
		Balance: info.Balance,
		Head:    st,
	})
	if err != nil {
		return xerrors.Errorf("setting account from actmap: %w", err)
	}
	return nil
}

func createMultisigAccount(ctx context.Context, bs bstore.Blockstore, cst cbor.IpldStore, state *state.StateTree, ida address.Address, info genesis.Actor, keyIDs map[address.Address]address.Address) error {
	if info.Type != genesis.TMultisig {
		return fmt.Errorf("can only call createMultisigAccount with multisig Actor info")
	}
	var ainfo genesis.MultisigMeta
	if err := json.Unmarshal(info.Meta, &ainfo); err != nil {
		return xerrors.Errorf("unmarshaling account meta: %w", err)
	}
	pending, err := adt2.MakeEmptyMap(adt2.WrapStore(ctx, cst)).Root()
	if err != nil {
		return xerrors.Errorf("failed to create empty map: %v", err)
	}

	var signers []address.Address

	for _, e := range ainfo.Signers {
		idAddress, ok := keyIDs[e]
		if !ok {
			return fmt.Errorf("no registered key ID for signer: %s", e)
		}

		// Check if actor already exists
		_, err := state.GetActor(e)
		if err == nil {
			signers = append(signers, idAddress)
			continue
		}

		st, err := cst.Put(ctx, &account2.State{Address: e})
		if err != nil {
			return err
		}
		err = state.SetActor(idAddress, &types.Actor{
			Code:    builtin2.AccountActorCodeID,
			Balance: types.NewInt(0),
			Head:    st,
		})
		if err != nil {
			return xerrors.Errorf("setting account from actmap: %w", err)
		}
		signers = append(signers, idAddress)
	}

	st, err := cst.Put(ctx, &multisig2.State{
		Signers:               signers,
		NumApprovalsThreshold: uint64(ainfo.Threshold),
		StartEpoch:            abi.ChainEpoch(ainfo.VestingStart),
		UnlockDuration:        abi.ChainEpoch(ainfo.VestingDuration),
		PendingTxns:           pending,
		InitialBalance:        ainfo.InitialVestingBalance(info.Balance),
	})
	if err != nil {
		return err
	}
	err = state.SetActor(ida, &types.Actor{
		Code:    builtin2.MultisigActorCodeID,
		Balance: info.Balance,
		Head:    st,
	})
	if err != nil {
		return xerrors.Errorf("setting account from actmap: %w", err)
	}
	return nil
}

func VerifyPreSealedData(ctx context.Context, cs *store.ChainStore, stateroot cid.Cid, template genesis.Template, keyIDs map[address.Address]address.Address) (cid.Cid, error) {
	/* verifNeeds := make(map[address.Address]abi.PaddedPieceSize)
	var sum abi.PaddedPieceSize */

	vmopt := vm.VMOpts{
		StateBase:      stateroot,
		Epoch:          0,
		Rand:           &fakeRand{},
		Bstore:         cs.Blockstore(),
		Syscalls:       mkFakedSigSyscalls(cs.VMSys()),
		CircSupplyCalc: nil,
		NtwkVersion:    genesisNetworkVersion,
		BaseFee:        types.NewInt(0),
	}
	vm, err := vm.NewVM(ctx, &vmopt)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create NewVM: %w", err)
	}

	for mi, m := range template.Miners {
		for si, s := range m.Sectors {
			if s.Deal.Provider != m.ID {
				return cid.Undef, xerrors.Errorf("Sector %d in miner %d in template had mismatch in provider and miner ID: %s != %s", si, mi, s.Deal.Provider, m.ID)
			}

			/* amt := s.Deal.PieceSize
			verifNeeds[keyIDs[s.Deal.Client]] += amt
			sum += amt */
		}
	}

	// Set initial governor
	_, err = doExecValue(ctx, vm, builtin2.GovernActorAddr, builtin.FoundationAddress, types.NewInt(0), builtin2.MethodsGovern.Grant, mustEnc(&govern.GrantOrRevokeParams{
		Governor:    builtin.FirstGovernorAddress,
		Authorities: nil, // Grant all priviledges to FirstGovernorAddress
	}))
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to set initial governor: %w", err)
	}

	/* verifregRoot, err := address.NewIDAddress(80)
	if err != nil {
		return cid.Undef, err
	}

	verifier, err := address.NewIDAddress(81)
	if err != nil {
		return cid.Undef, err
	}

	_, err = doExecValue(ctx, vm, builtin2.VerifiedRegistryActorAddr, verifregRoot, types.NewInt(0), builtin2.MethodsVerifiedRegistry.AddVerifier, mustEnc(&verifreg2.AddVerifierParams{

		Address:   verifier,
		Allowance: abi.NewStoragePower(int64(sum)), // eh, close enough

	}))
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create verifier: %w", err)
	}

	for c, amt := range verifNeeds {
		_, err := doExecValue(ctx, vm, builtin2.VerifiedRegistryActorAddr, verifier, types.NewInt(0), builtin2.MethodsVerifiedRegistry.AddVerifiedClient, mustEnc(&verifreg2.AddVerifiedClientParams{
			Address:   c,
			Allowance: abi.NewStoragePower(int64(amt)),
		}))
		if err != nil {
			return cid.Undef, xerrors.Errorf("failed to add verified client: %w", err)
		}
	} */

	st, err := vm.Flush(ctx)
	if err != nil {
		return cid.Cid{}, xerrors.Errorf("vm flush: %w", err)
	}

	return st, nil
}

func MakeGenesisBlock(ctx context.Context, j journal.Journal, bs bstore.Blockstore, sys vm.SyscallBuilder, template genesis.Template) (*GenesisBootstrap, error) {
	if j == nil {
		j = journal.NilJournal()
	}
	st, keyIDs, err := MakeInitialStateTree(ctx, bs, template)
	if err != nil {
		return nil, xerrors.Errorf("make initial state tree failed: %w", err)
	}

	stateroot, err := st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("flush state tree failed: %w", err)
	}

	// temp chainstore
	cs := store.NewChainStore(bs, bs, datastore.NewMapDatastore(), sys, j)

	// Verify PreSealed Data
	stateroot, err = VerifyPreSealedData(ctx, cs, stateroot, template, keyIDs)
	if err != nil {
		return nil, xerrors.Errorf("failed to verify presealed data: %w", err)
	}

	stateroot, err = SetupStorageMiners(ctx, cs, stateroot, template.Miners)
	if err != nil {
		return nil, xerrors.Errorf("setup miners failed: %w", err)
	}

	store := adt2.WrapStore(ctx, cbor.NewCborStore(bs))
	emptyroot, err := adt2.MakeEmptyArray(store).Root()
	if err != nil {
		return nil, xerrors.Errorf("amt build failed: %w", err)
	}

	mm := &types.MsgMeta{
		BlsMessages:   emptyroot,
		SecpkMessages: emptyroot,
	}
	mmb, err := mm.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing msgmeta failed: %w", err)
	}
	if err := bs.Put(mmb); err != nil {
		return nil, xerrors.Errorf("putting msgmeta block to blockstore: %w", err)
	}

	log.Infof("Empty Genesis root: %s", emptyroot)

	tickBuf := make([]byte, 32)
	_, _ = rand.Read(tickBuf)
	genesisticket := &types.Ticket{
		VRFProof: tickBuf,
	}

	epikGenesisCid, err := cid.Decode(epikGenesisCidString)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode epik genesis block CID: %w", err)
	}

	if !expectedCid().Equals(epikGenesisCid) {
		return nil, xerrors.Errorf("expectedCid != epikGenesisCid")
	}

	gblk, err := getGenesisBlock()
	if err != nil {
		return nil, xerrors.Errorf("failed to construct epik genesis block: %w", err)
	}

	if !epikGenesisCid.Equals(gblk.Cid()) {
		return nil, xerrors.Errorf("epikGenesisCid != gblk.Cid")
	}

	if err := bs.Put(gblk); err != nil {
		return nil, xerrors.Errorf("failed writing epik genesis block to blockstore: %w", err)
	}

	b := &types.BlockHeader{
		Miner:                 builtin2.SystemActorAddr,
		Ticket:                genesisticket,
		Parents:               []cid.Cid{epikGenesisCid},
		Height:                0,
		ParentWeight:          types.NewInt(0),
		ParentStateRoot:       stateroot,
		Messages:              mmb.Cid(),
		ParentMessageReceipts: emptyroot,
		BLSAggregate:          nil,
		BlockSig:              nil,
		Timestamp:             template.Timestamp,
		ElectionProof:         new(types.ElectionProof),
		BeaconEntries: []types.BeaconEntry{
			{
				Round: 0,
				Data:  make([]byte, 32),
			},
		},
		ParentBaseFee: abi.NewTokenAmount(build.InitialBaseFee),
	}

	sb, err := b.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing block header failed: %w", err)
	}

	if err := bs.Put(sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &GenesisBootstrap{
		Genesis: b,
	}, nil
}
