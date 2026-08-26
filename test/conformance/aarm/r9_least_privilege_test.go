package aarmconformance_test

import (
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
)

func TestR9_JITCredentialIssuance(t *testing.T) {
	aarm.Requires(t, aarm.R9, aarm.SHOULD, "Just-in-time credential issuance for agent actions")
	aarm.Skip(t, aarm.OutOfScope, "Gryph does not issue credentials; least-privilege capture happens in an IdP")
}

func TestR9_CredentialScopingPerAction(t *testing.T) {
	aarm.Requires(t, aarm.R9, aarm.SHOULD, "Credentials scoped per action")
	aarm.Skip(t, aarm.OutOfScope, "credential scoping is an IdP / secrets-broker concern")
}

func TestR9_CredentialIssuanceLogged(t *testing.T) {
	aarm.Requires(t, aarm.R9, aarm.SHOULD, "Credential issuance logged on the receipt")
	aarm.Skip(t, aarm.OutOfScope, "no credential issuance path inside Gryph")
}
