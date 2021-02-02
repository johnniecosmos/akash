package keeper

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ovrclk/akash/x/escrow/types"
)

type AccountHook func(sdk.Context, types.Account)
type PaymentHook func(sdk.Context, types.Payment)

type Keeper interface {
	AccountCreate(ctx sdk.Context, id types.AccountID, owner sdk.AccAddress, deposit sdk.Coin) error
	AccountDeposit(ctx sdk.Context, id types.AccountID, amount sdk.Coin) error
	AccountSettle(ctx sdk.Context, id types.AccountID) error
	AccountClose(ctx sdk.Context, id types.AccountID) error
	PaymentCreate(ctx sdk.Context, id types.AccountID, pid string, owner sdk.AccAddress, rate sdk.Coin) error
	PaymentWithdraw(ctx sdk.Context, id types.AccountID) error
	Paymentclose(ctx sdk.Context, id types.AccountID) error
	AddOnAccountClosedHook(AccountHook) Keeper
	AddOnPaymentClosedHook(PaymentHook) Keeper
}

func NewKeeper(cdc codec.BinaryMarshaler, skey sdk.StoreKey, bkeeper BankKeeper) Keeper {
	return &keeper{
		cdc:     cdc,
		skey:    skey,
		bkeeper: bkeeper,
	}
}

type keeper struct {
	cdc     codec.BinaryMarshaler
	skey    sdk.StoreKey
	bkeeper BankKeeper

	hooks struct {
		onAccountClosed []AccountHook
		onPaymentClosed []PaymentHook
	}
}

func (k *keeper) AccountCreate(ctx sdk.Context, id types.AccountID, owner sdk.AccAddress, deposit sdk.Coin) error {
	store := ctx.KVStore(k.skey)
	key := accountKey(id)

	if store.Has(key) {
		return fmt.Errorf("exists")
	}

	if err := k.bkeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, sdk.NewCoins(deposit)); err != nil {
		return err
	}

	obj := &types.Account{
		ID:          id,
		Owner:       owner.String(),
		State:       types.AccountOpen,
		Balance:     deposit,
		Transferred: sdk.NewCoin(deposit.Denom, sdk.ZeroInt()),
		SettledAt:   ctx.BlockHeight(),
	}

	store.Set(key, k.cdc.MustMarshalBinaryBare(obj))

	return nil
}

func (k *keeper) AccountDeposit(ctx sdk.Context, id types.AccountID, amount sdk.Coin) error {
	store := ctx.KVStore(k.skey)
	key := accountKey(id)

	buf := store.Get(key)

	if len(buf) == 0 {
		return fmt.Errorf("not found")
	}

	var obj types.Account

	k.cdc.MustUnmarshalBinaryBare(buf, &obj)

	owner, err := sdk.AccAddressFromBech32(obj.Owner)
	if err != nil {
		return err
	}

	if err := k.bkeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, sdk.NewCoins(amount)); err != nil {
		return err
	}

	obj.Balance = obj.Balance.Add(amount)

	store.Set(key, k.cdc.MustMarshalBinaryBare(&obj))

	return nil
}

func (k *keeper) AccountSettle(ctx sdk.Context, id types.AccountID) error {
	return nil
}

func (k *keeper) AccountClose(ctx sdk.Context, id types.AccountID) error {
	return nil
}

func (k *keeper) PaymentCreate(ctx sdk.Context, id types.AccountID, pid string, owner sdk.AccAddress, rate sdk.Coin) error {
	store := ctx.KVStore(k.skey)
	key := paymentKey(id, pid)

	if err := k.AccountSettle(ctx, id); err != nil {
		return err
	}

	// TODO: ensure rate denomination is same as account denomination

	if store.Has(key) {
		return fmt.Errorf("exists")
	}

	obj := &types.Payment{
		AccountID: id,
		PaymentID: pid,
		Owner:     owner.String(),
		State:     types.PaymentOpen,
		Rate:      rate,
		Balance:   sdk.NewCoin(rate.Denom, sdk.ZeroInt()),
		Withdrawn: sdk.NewCoin(rate.Denom, sdk.ZeroInt()),
	}

	store.Set(key, k.cdc.MustMarshalBinaryBare(obj))

	return nil
}

func (k *keeper) PaymentWithdraw(ctx sdk.Context, id types.AccountID) error {
	store := ctx.KVStore(k.skey)
	key := paymentKey(id, pid)

	if !store.Has(key) {
		return fmt.Errorf("doesn't exist")
	}

	if err := k.AccountSettle(ctx, id); err != nil {
		// TODO: always withdraw even if there's an error
		return err
	}

	buf := store.Get(key)

	if len(buf) == 0 {
		return fmt.Errorf("not found")
	}

	var obj types.Payment

	k.cdc.MustUnmarshalBinaryBare(buf, &obj)

	owner, err := sdk.AccAddressFromBech32(obj.Owner)
	if err != nil {
		return err
	}

	if obj.Balance.IsZero() {
		return nil
	}

	if err := k.bkeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, sdk.NewCoins(obj.Balance)); err != nil {
		return err
	}

	obj.Withdrawn = obj.Withdrawn.Add(obj.Balance)
	obj.Balance = sdk.NewCoin(obj.Balance.Denom, sdk.ZeroInt())

	store.Set(key, k.cdc.MustMarshalBinaryBare(obj))

	return nil
}

func (k *keeper) Paymentclose(ctx sdk.Context, id types.AccountID) error {
	return nil
}

func (k *keeper) AddOnAccountClosedHook(hook AccountHook) Keeper {
	k.hooks.onAccountClosed = append(k.hooks.onAccountClosed, hook)
	return k
}

func (k *keeper) AddOnPaymentClosedHook(hook PaymentHook) Keeper {
	k.hooks.onPaymentClosed = append(k.hooks.onPaymentClosed, hook)
	return k
}
