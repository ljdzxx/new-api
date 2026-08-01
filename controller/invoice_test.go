package controller

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInvoiceAmountMeetsMinimum(t *testing.T) {
	tests := []struct {
		name      string
		amount    float64
		minimum   float64
		qualified bool
	}{
		{name: "below minimum", amount: 299.99, minimum: 300, qualified: false},
		{name: "equal to minimum", amount: 300, minimum: 300, qualified: true},
		{name: "above minimum", amount: 300.01, minimum: 300, qualified: true},
		{name: "invalid amount", amount: math.NaN(), minimum: 300, qualified: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.qualified, invoiceAmountMeetsMinimum(test.amount, test.minimum))
		})
	}
}
