package types

import (
	fmt "fmt"

	etypes "github.com/ovrclk/akash/x/escrow/types"
)

const (
	bidEscrowScope = "bid"
)

func EscrowAccountForBid(id BidID) etypes.AccountID {
	return etypes.AccountID{
		Scope: bidEscrowScope,
		XID:   id.String(),
	}
}

func EscrowPaymentForBid(id BidID) string {
	return fmt.Sprintf("%v/%v/%s", id.OSeq, id.GSeq, id.Provider)
}
