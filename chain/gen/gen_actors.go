package gen

import (
	"github.com/EpiK-Protocol/go-epik/chain/types"
	"github.com/EpiK-Protocol/go-epik/genesis"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
)

// For testing
//
// PrivateKey of t1dnas3yoc5bvz5evcuocb7tudimn2tpz63ajlk4y:
//
// 7b2254797065223a22736563703235366b31222c22507269766174654b6579223a226e4a592b41555649724f596e6a51452f675653565274444f434374686d39785a4d7162764f7546794f69413d227d
//

/////////////////
//	allocation
/////////////////

// team & contributors
var DefaultTeamAccountActor = genesis.Actor{
	Type:    genesis.TMultisig,
	Balance: types.FromEpk(50_000_000), // 50M
	Meta: (&genesis.MultisigMeta{
		Signers: []address.Address{
			makeAddress("t1dnas3yoc5bvz5evcuocb7tudimn2tpz63ajlk4y"),
		},
		Threshold:       1,
		VestingDuration: 90 * 15 * builtin.EpochsInDay,
		VestingStart:    0,
		InitialVestedTarget: &builtin.BigFrac{
			Numerator:   big.NewInt(1),
			Denominator: big.NewInt(16),
		},
	}).ActorMeta(),
}

// foundation
var DefaultFoundationAccountActor = genesis.Actor{
	Type:    genesis.TMultisig,
	Balance: types.FromEpk(100_000_000), // may be a little less than 100M
	Meta: (&genesis.MultisigMeta{
		Signers: []address.Address{
			makeAddress("t1dnas3yoc5bvz5evcuocb7tudimn2tpz63ajlk4y"),
		},
		Threshold:       1,
		VestingDuration: 90 * 7 * builtin.EpochsInDay,
		VestingStart:    0,
		InitialVestedTarget: &builtin.BigFrac{
			Numerator:   big.NewInt(1),
			Denominator: big.NewInt(8),
		},
	}).ActorMeta(),
}

// fundraising
var DefaultFundraisingAccountActor = genesis.Actor{
	Type:    genesis.TMultisig,
	Balance: types.FromEpk(100_000_000), //  150M
	Meta: (&genesis.MultisigMeta{
		Signers: []address.Address{
			makeAddress("t1dnas3yoc5bvz5evcuocb7tudimn2tpz63ajlk4y"),
		},
		Threshold:       1,
		VestingDuration: 90 * 7 * builtin.EpochsInDay,
		VestingStart:    0,
		InitialVestedTarget: &builtin.BigFrac{
			Numerator:   big.NewInt(1),
			Denominator: big.NewInt(8),
		},
	}).ActorMeta(),
}

/////////////////
// 	governor
/////////////////
var FirstGovernorAccountActor = genesis.Actor{
	Type:    genesis.TMultisig,
	Balance: big.Zero(),
	Meta: (&genesis.MultisigMeta{
		Signers: []address.Address{
			makeAddress("t1dnas3yoc5bvz5evcuocb7tudimn2tpz63ajlk4y"),
		},
		Threshold:       1,
		VestingDuration: 0,
		VestingStart:    0,
	}).ActorMeta(),
}

func makeAddress(addr string) address.Address {
	ret, err := address.NewFromString(addr)
	if err != nil {
		panic(err)
	}

	return ret
}