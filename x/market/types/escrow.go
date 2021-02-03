package types

import fmt "fmt"

func EscrowPaymentForBid(id BidID) string {
	return fmt.Sprintf("%v/%v/%s", id.OSeq, id.GSeq, id.Provider)
}
