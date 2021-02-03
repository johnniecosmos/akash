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
	AccountSettle(ctx sdk.Context, id types.AccountID) (bool, error)
	AccountClose(ctx sdk.Context, id types.AccountID) error
	PaymentCreate(ctx sdk.Context, id types.AccountID, pid string, owner sdk.AccAddress, rate sdk.Coin) error
	PaymentWithdraw(ctx sdk.Context, id types.AccountID, pid string) error
	PaymentClose(ctx sdk.Context, id types.AccountID, pid string) error
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

	obj, err := k.getAccount(ctx, id)
	if err != nil {
		return err
	}

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

func (k *keeper) AccountSettle(ctx sdk.Context, id types.AccountID) (bool, error) {

	account, err := k.getAccount(ctx, id)

	if err != nil {
		return false, err
	}

	if account.State != types.AccountOpen {
		return false, fmt.Errorf("invalid state")
	}

	heightDelta := sdk.NewInt(ctx.BlockHeight() - account.SettledAt)

	if heightDelta.IsZero() {
		return false, nil
	}

	payments := k.accountOpenPayments(ctx, id)

	if len(payments) == 0 {
		return false, nil
	}

	blockRate := sdk.NewCoin(account.Balance.Denom, sdk.ZeroInt())

	for _, payment := range payments {
		blockRate = blockRate.Add(payment.Rate)
	}

	totalTransfer := blockRate.Amount.Mul(heightDelta)

	accountBalance := account.Balance

	numFullBlocks := accountBalance.Amount.Quo(blockRate.Amount)

	if numFullBlocks.GT(heightDelta) {
		numFullBlocks = heightDelta
	}

	for idx := range payments {
		p := payments[idx]
		payments[idx].Balance = p.Balance.Add(
			sdk.NewCoin(p.Rate.Denom, p.Rate.Amount.Mul(numFullBlocks)))
	}

	// all payments made in full
	if numFullBlocks.Equal(heightDelta) {
		account.SettledAt = ctx.BlockHeight()
		account.Transferred = account.Transferred.Add(
			sdk.NewCoin(account.Transferred.Denom, totalTransfer))
		account.Balance = account.Balance.Sub(
			sdk.NewCoin(account.Balance.Denom, totalTransfer))

		// save objects
		k.saveAccount(ctx, &account)
		for idx := range payments {
			k.savePayment(ctx, &payments[idx])
		}

		// return early
		return false, nil
	}

	// overdrawn

	remainingAmount := sdk.NewDecFromInt(accountBalance.Amount.Sub(blockRate.Amount.Mul(numFullBlocks)))
	distributedAmount := sdk.ZeroDec()

	// distribute remaining balance weighted by rate
	for idx := range payments {
		payment := payments[idx]
		// amount := (rate / blockrate) * remaining
		amount := remainingAmount.
			MulInt(payment.Rate.Amount).
			QuoInt(blockRate.Amount).
			TruncateInt()

		payments[idx].Balance = payment.Balance.Add(sdk.NewCoin(payment.Balance.Denom, amount))
		payments[idx].State = types.PaymentOverdrawn

		distributedAmount = distributedAmount.Add(sdk.NewDecFromInt(amount))
	}

	// distribute any remaining amount evenly
	remainder := remainingAmount.Sub(distributedAmount)
	for idx := range payments {
		if remainder.IsZero() {
			break
		}
		payment := payments[idx]

		amount := remainder.QuoInt64(int64(len(payments) - idx)).Ceil()
		payments[idx].Balance = payment.Balance.Add(
			sdk.NewCoin(payment.Balance.Denom, amount.TruncateInt()))
		remainder = remainder.Sub(amount)
	}

	account.Balance = account.Balance.Sub(account.Balance)
	account.Transferred = account.Transferred.Add(
		sdk.NewCoin(account.Transferred.Denom, remainingAmount.TruncateInt()))
	account.SettledAt = ctx.BlockHeight()
	account.State = types.AccountOverdrawn

	// save objects
	k.saveAccount(ctx, &account)
	for idx := range payments {
		k.savePayment(ctx, &payments[idx])
		k.paymentWithdraw(ctx, &payments[idx])
	}

	// call hooks
	for _, hook := range k.hooks.onAccountClosed {
		hook(ctx, account)
	}

	for _, hook := range k.hooks.onPaymentClosed {
		for _, payment := range payments {
			hook(ctx, payment)
		}
	}

	return true, nil
}

func (k *keeper) AccountClose(ctx sdk.Context, id types.AccountID) error {
	account, err := k.getAccount(ctx, id)

	if account.State != types.AccountOpen {
		return fmt.Errorf("invalid state")
	}

	od, err := k.AccountSettle(ctx, id)
	if err != nil {
		return err
	}
	if od {
		return nil
	}

	account.State = types.AccountClosed
	if err := k.accountWithdraw(ctx, &account); err != nil {
		return err
	}

	payments := k.accountOpenPayments(ctx, id)

	for idx := range payments {
		payments[idx].State = types.PaymentClosed
		k.paymentWithdraw(ctx, &payments[idx])
	}

	for _, hook := range k.hooks.onAccountClosed {
		hook(ctx, account)
	}

	for _, hook := range k.hooks.onPaymentClosed {
		for idx := range payments {
			hook(ctx, payments[idx])
		}
	}

	return nil
}

func (k *keeper) PaymentCreate(ctx sdk.Context, id types.AccountID, pid string, owner sdk.AccAddress, rate sdk.Coin) error {

	od, err := k.AccountSettle(ctx, id)
	if err != nil {
		return err
	}

	if od {
		return fmt.Errorf("account overdrawn")
	}

	account, err := k.getAccount(ctx, id)
	if err != nil {
		return err
	}

	if rate.Denom != account.Balance.Denom {
		return fmt.Errorf("invalid denom")
	}

	store := ctx.KVStore(k.skey)
	key := paymentKey(id, pid)

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

func (k *keeper) PaymentWithdraw(ctx sdk.Context, id types.AccountID, pid string) error {
	payment, err := k.getPayment(ctx, id, pid)
	if err != nil {
		return err
	}

	od, err := k.AccountSettle(ctx, id)
	if err != nil {
		return err
	}
	if od {
		return nil
	}

	return k.paymentWithdraw(ctx, &payment)
}

func (k *keeper) PaymentClose(ctx sdk.Context, id types.AccountID, pid string) error {
	payment, err := k.getPayment(ctx, id, pid)
	if err != nil {
		return err
	}

	if payment.State != types.PaymentOpen {
		return fmt.Errorf("invalid state")
	}

	od, err := k.AccountSettle(ctx, id)

	if err != nil {
		return err
	}
	if od {
		return nil
	}

	payment, err = k.getPayment(ctx, id, pid)
	if err != nil {
		return err
	}

	payment.State = types.PaymentClosed

	if err := k.paymentWithdraw(ctx, &payment); err != nil {
		return err
	}

	for _, hook := range k.hooks.onPaymentClosed {
		hook(ctx, payment)
	}

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

func (k *keeper) getAccount(ctx sdk.Context, id types.AccountID) (types.Account, error) {

	store := ctx.KVStore(k.skey)
	key := accountKey(id)

	if !store.Has(key) {
		return types.Account{}, fmt.Errorf("doesn't exist")
	}

	buf := store.Get(key)

	var obj types.Account

	k.cdc.MustUnmarshalBinaryBare(buf, &obj)

	return obj, nil
}

func (k *keeper) getPayment(ctx sdk.Context, id types.AccountID, pid string) (types.Payment, error) {
	store := ctx.KVStore(k.skey)
	key := paymentKey(id, pid)

	if !store.Has(key) {
		return types.Payment{}, fmt.Errorf("doesn't exist")
	}

	buf := store.Get(key)

	var obj types.Payment

	k.cdc.MustUnmarshalBinaryBare(buf, &obj)

	return obj, nil
}

func (k *keeper) saveAccount(ctx sdk.Context, obj *types.Account) {
	store := ctx.KVStore(k.skey)
	key := accountKey(obj.ID)
	store.Set(key, k.cdc.MustMarshalBinaryBare(obj))
}

func (k *keeper) savePayment(ctx sdk.Context, obj *types.Payment) {
	store := ctx.KVStore(k.skey)
	key := paymentKey(obj.AccountID, obj.PaymentID)
	store.Set(key, k.cdc.MustMarshalBinaryBare(obj))
}

func (k *keeper) accountPayments(ctx sdk.Context, id types.AccountID) []types.Payment {
	store := ctx.KVStore(k.skey)
	iter := sdk.KVStorePrefixIterator(store, accountPaymentsKey(id))

	var payments []types.Payment

	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var val types.Payment
		k.cdc.MustUnmarshalBinaryBare(iter.Value(), &val)
		payments = append(payments, val)
	}

	return payments
}

func (k *keeper) accountOpenPayments(ctx sdk.Context, id types.AccountID) []types.Payment {
	var payments []types.Payment
	for _, payment := range k.accountPayments(ctx, id) {
		if payment.State != types.PaymentOpen {
			continue
		}
		payments = append(payments, payment)
	}
	return payments
}

func (k *keeper) accountWithdraw(ctx sdk.Context, obj *types.Account) error {
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
	obj.Balance = sdk.NewCoin(obj.Balance.Denom, sdk.ZeroInt())

	k.saveAccount(ctx, obj)
	return nil
}

func (k *keeper) paymentWithdraw(ctx sdk.Context, obj *types.Payment) error {

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

	k.savePayment(ctx, obj)
	return nil
}
