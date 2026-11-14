package main

import (
	"fmt"
	"strconv"
	"testing"
)

// parseTLV walks a payload and returns tag -> value (top level only).
func parseTLV(t *testing.T, s string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for i := 0; i < len(s); {
		if i+4 > len(s) {
			t.Fatalf("truncated field at %d in %q", i, s)
		}
		n, err := strconv.Atoi(s[i+2 : i+4])
		if err != nil || i+4+n > len(s) {
			t.Fatalf("bad length at %d in %q", i, s)
		}
		out[s[i:i+2]] = s[i+4 : i+4+n]
		i += 4 + n
	}
	return out
}

func TestCRC16CheckValue(t *testing.T) {
	// the published check value of CRC-16/CCITT-FALSE
	if got := crc16CCITT("123456789"); got != 0x29B1 {
		t.Fatalf("crc = %04X, want 29B1", got)
	}
}

func TestPayloadKnownValues(t *testing.T) {
	// regression vectors (checked against an independent port of the same rules).
	// Before going live, scan a real one with a banking app and pay a small amount.
	cases := []struct{ id, amount, want string }{
		{"081-234-5678", "100", "00020101021229370016A0000006770101110113006681234567853037645406100.005802TH6304F142"},
		{"0812345678", "12900.00", "00020101021229370016A000000677010111011300668123456785303764540812900.005802TH6304095B"},
		{"0105551234567", "", "00020101021129370016A0000006770101110213010555123456753037645802TH63046115"},
	}
	for _, c := range cases {
		got, err := BuildPromptPayPayload(c.id, c.amount)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if got != c.want {
			t.Errorf("%s %q:\n got  %s\n want %s", c.id, c.amount, got, c.want)
		}
	}
}

func TestPayloadStructureAndChecksum(t *testing.T) {
	p, err := BuildPromptPayPayload("0812345678", "250.5")
	if err != nil {
		t.Fatal(err)
	}
	f := parseTLV(t, p)
	if f["00"] != "01" || f["01"] != "12" || f["53"] != "764" || f["58"] != "TH" {
		t.Fatalf("unexpected header fields: %v", f)
	}
	if f["54"] != "250.50" {
		t.Fatalf("amount = %q, want 250.50", f["54"])
	}
	m := parseTLV(t, f["29"])
	if m["00"] != "A000000677010111" || m["01"] != "0066812345678" {
		t.Fatalf("merchant info wrong: %v", m)
	}
	// the last 4 characters are the CRC of everything before them
	body, crc := p[:len(p)-4], p[len(p)-4:]
	if want := fmt.Sprintf("%04X", crc16CCITT(body)); crc != want {
		t.Fatalf("crc %s, want %s", crc, want)
	}
	// static QR when no amount: point of initiation 11 and no amount field
	s, err := BuildPromptPayPayload("0812345678", "")
	if err != nil {
		t.Fatal(err)
	}
	sf := parseTLV(t, s)
	if sf["01"] != "11" {
		t.Fatalf("static QR should use 11, got %q", sf["01"])
	}
	if _, has := sf["54"]; has {
		t.Fatal("static QR must not carry an amount")
	}
}

func TestPayloadRejectsBadInput(t *testing.T) {
	for _, id := range []string{"", "12345", "08123456", "08123x5678", "+66812345678", "01234567890123456789"} {
		if _, err := BuildPromptPayPayload(id, "100"); err == nil {
			t.Errorf("id %q should be rejected", id)
		}
	}
	for _, amt := range []string{"0", "-5", "abc", "1e400"} {
		if _, err := BuildPromptPayPayload("0812345678", amt); err == nil {
			t.Errorf("amount %q should be rejected", amt)
		}
	}
	if err := validPromptPayID("0812345678"); err != nil {
		t.Errorf("valid id rejected: %v", err)
	}
}
