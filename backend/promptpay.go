package main

// PromptPay QR payload (EMVCo merchant-presented QR, Thai "credit transfer" profile).
// Pure functions, standard library only: no database, no network, so it is unit-tested on its own
// (promptpay_test.go). The frontend turns the returned text into a QR image.
//
// The customer's banking app reads the payload, so the shop's PromptPay ID and the amount are in it.
// There is NO automatic confirmation with this kind of QR: a person (or a payment gateway) must confirm.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// emvField encodes one EMVCo field: 2-digit tag, 2-digit length, value.
func emvField(tag, value string) string {
	return fmt.Sprintf("%s%02d%s", tag, len(value), value)
}

// crc16CCITT is CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF), the checksum EMVCo uses.
func crc16CCITT(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for b := 0; b < 8; b++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// promptPayTarget turns what the shop typed (phone, national/tax ID, e-wallet ID) into the sub-tag and value.
//   - 10 digits starting with 0 -> mobile number, sent as 0066 + number without the leading 0 (sub-tag 01)
//   - 13 digits                  -> national ID or tax ID (sub-tag 02)
//   - 15 digits                  -> e-wallet ID (sub-tag 03)
func promptPayTarget(id string) (subTag, value string, err error) {
	var digits strings.Builder
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '-' || r == ' ':
			// allowed separators, e.g. 081-234-5678
		default:
			return "", "", errors.New("PromptPay ID may contain only digits, spaces and dashes")
		}
	}
	d := digits.String()
	switch {
	case len(d) == 10 && d[0] == '0':
		return "01", "0066" + d[1:], nil
	case len(d) == 13:
		return "02", d, nil
	case len(d) == 15:
		return "03", d, nil
	}
	return "", "", errors.New("PromptPay ID must be a 10-digit mobile number (starting with 0), a 13-digit national/tax ID or a 15-digit e-wallet ID")
}

// validPromptPayID reports whether id can be used with BuildPromptPayPayload.
func validPromptPayID(id string) error {
	_, _, err := promptPayTarget(id)
	return err
}

// BuildPromptPayPayload returns the text to encode in the QR. amount is a decimal string such as "12900.00".
// Empty amount gives a static QR (the customer types the amount); a non-empty one gives a dynamic QR.
func BuildPromptPayPayload(id, amount string) (string, error) {
	subTag, value, err := promptPayTarget(id)
	if err != nil {
		return "", err
	}
	poi := "11" // static
	var amountField string
	if strings.TrimSpace(amount) != "" {
		f, err := strconv.ParseFloat(strings.TrimSpace(amount), 64)
		if err != nil || f <= 0 || f > 9999999999.99 {
			return "", errors.New("amount must be a number greater than 0")
		}
		poi = "12" // dynamic: the amount is fixed
		amountField = emvField("54", strconv.FormatFloat(f, 'f', 2, 64))
	}
	merchant := emvField("00", "A000000677010111") + emvField(subTag, value)
	payload := emvField("00", "01") + emvField("01", poi) + emvField("29", merchant) + emvField("53", "764") + amountField + emvField("58", "TH")
	payload += "6304"
	return payload + fmt.Sprintf("%04X", crc16CCITT(payload)), nil
}
