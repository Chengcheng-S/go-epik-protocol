// +build testground

// This file makes hardcoded parameters (const) configurable as vars.
//
// Its purpose is to unlock various degrees of flexibility and parametrization
// when writing Testground plans for epik.
//
package build

import (
	"math/big"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"

	builtin2 "github.com/filecoin-project/specs-actors/v2/actors/builtin"

	"github.com/EpiK-Protocol/go-epik/chain/actors/policy"
)

var (
	UnixfsChunkSize     = uint64(1 << 20)
	UnixfsLinksPerLevel = 1024

	BlocksPerEpoch        = uint64(builtin2.ExpectedLeadersPerEpoch)
	BlockMessageLimit     = 512
	BlockGasLimit         = int64(100_000_000_000)
	BlockGasTarget        = int64(BlockGasLimit / 2)
	BaseFeeMaxChangeDenom = int64(8) // 12.5%
	InitialBaseFee        = int64(100e6)
	MinimumBaseFee        = int64(100)
	BlockDelaySecs        = uint64(builtin2.EpochDurationSeconds)
	PropagationDelaySecs  = uint64(6)

	AllowableClockDriftSecs = uint64(1)

	Finality            = policy.ChainFinality
	ForkLengthThreshold = Finality

	SlashablePowerDelay        = 20
	InteractivePoRepConfidence = 6

	MessageConfidence uint64 = 5

	WRatioNum = int64(1)
	WRatioDen = uint64(2)

	BadBlockCacheSize     = 1 << 15
	BlsSignatureCacheSize = 40000
	VerifSigCacheSize     = 32000

	SealRandomnessLookback = policy.SealRandomnessLookback

	TicketRandomnessLookback = abi.ChainEpoch(1)

	EpkBase               uint64 = 1_000_000_000
	EpkAllocStorageMining uint64 = 700_000_000
	// EpkReserved           uint64 = 300_000_000

	EpkPrecision uint64 = 1_000_000_000_000_000_000

	InitialRewardBalance = func() *big.Int {
		v := big.NewInt(int64(EpkAllocStorageMining))
		v = v.Mul(v, big.NewInt(int64(EpkPrecision)))
		return v
	}()

	// InitialEpkReserved = func() *big.Int {
	// 	v := big.NewInt(int64(EpkReserved))
	// 	v = v.Mul(v, big.NewInt(int64(FilecoinPrecision)))
	// 	return v
	// }()

	// Actor consts
	// TODO: remove it
	MinDealDuration = abi.ChainEpoch(180 * builtin2.EpochsInDay)

	PackingEfficiencyNum   int64 = 4
	PackingEfficiencyDenom int64 = 5
	/*
		UpgradeBreezeHeight      abi.ChainEpoch = -1
		BreezeGasTampingDuration abi.ChainEpoch = 0

		UpgradeSmokeHeight    abi.ChainEpoch = -1
		UpgradeIgnitionHeight abi.ChainEpoch = -2
		UpgradeRefuelHeight   abi.ChainEpoch = -3
		UpgradeTapeHeight     abi.ChainEpoch = -4
		UpgradeActorsV2Height abi.ChainEpoch = 10
		UpgradeLiftoffHeight  abi.ChainEpoch = -5
		UpgradeKumquatHeight  abi.ChainEpoch = -6
		UpgradeCalicoHeight   abi.ChainEpoch = -7
		UpgradePersianHeight  abi.ChainEpoch = -8
		UpgradeOrangeHeight   abi.ChainEpoch = -9
		UpgradeClausHeight    abi.ChainEpoch = -10
		UpgradeActorsV3Height abi.ChainEpoch = -11
	*/
	DrandSchedule = map[abi.ChainEpoch]DrandEnum{
		0: DrandMainnet,
	}

	NewestNetworkVersion = network.Version10
	// ActorUpgradeNetworkVersion = network.Version4

	Devnet      = true
	ZeroAddress = MustParseAddress("f3yaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaby2smx7a")

	BootstrappersFile = ""
	GenesisFile       = ""
)

const BootstrapPeerThreshold = 1
