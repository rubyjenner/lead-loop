package main

import (
	"strings"
	"testing"
)

func TestFactsIncludeProductFactsOnlyWhenGiven(t *testing.T) {
	ci := campaignInfo{Name: "Sale", Product: "iPhone 12", Variants: "64GB / ดำ", PaymentMethods: "โอน, บัตรเครดิต", AfterSales: "  "}
	f := ci.facts()
	for _, want := range []string{"รุ่น/ขนาด/สี: 64GB / ดำ", "ช่องทางชำระเงิน: โอน, บัตรเครดิต"} {
		if !strings.Contains(f, want) {
			t.Errorf("facts should contain %q:\n%s", want, f)
		}
	}
	for _, absent := range []string{"บริการหลังการขาย", "การผ่อนชำระ", "แบบสินค้า"} {
		if strings.Contains(f, absent) {
			t.Errorf("an empty field must not appear (the AI would be tempted to invent it): %q in\n%s", absent, f)
		}
	}
	if strings.Contains(campaignInfo{Name: "x"}.facts(), "ช่องทางชำระเงิน") {
		t.Error("no product -> no product facts")
	}
}
