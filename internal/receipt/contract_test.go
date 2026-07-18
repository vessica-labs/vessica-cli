package receipt

import (
	"strings"
	"testing"
)

func TestContextContractUsesReceiptFieldConstants(t *testing.T) {
	contract := ContextContract()
	for _, field := range []string{FieldElapsed, FieldWallElapsed, FieldInfrastructure} {
		if !strings.Contains(contract, field) {
			t.Fatalf("contract missing %q: %s", field, contract)
		}
	}
}
