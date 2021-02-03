package types

import (
	etypes "github.com/ovrclk/akash/x/escrow/types"
)

const (
	deploymentEscrowScope = "deployment"
)

func EscrowAccountForDeployment(id DeploymentID) etypes.AccountID {
	return etypes.AccountID{
		Scope: deploymentEscrowScope,
		XID:   id.String(),
	}
}
