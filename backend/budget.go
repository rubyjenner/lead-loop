package main

// ExtractBudget reads the budget a customer states in a message, for example
//
//	"งบ 15,000"  "งบประมาณไม่เกิน 2 หมื่น"  "มีงบสัก 8 พัน"  "ไม่เกิน 12000 บาท"  "งบ ๑๐๐๐๐"
//
// Only a number that follows a budget word (งบ, งบประมาณ, ไม่เกิน) counts, so "ราคา 12900 บาทใช่ไหม" or a phone number
// is never read as a budget. Only the number is kept (leads.budget); the text itself is not stored.

import (
	"regexp"
	"strconv"
	"strings"
)

var budgetRe = regexp.MustCompile(`(?:งบประมาณ|งบ|ไม่เกิน)\D{0,14}?([0-9][0-9,]*(?:\.[0-9]+)?)\s*(พัน|หมื่น|แสน|ล้าน|[kK]\b)?`)

const (
	budgetMin = 100.0       // below this it is probably not money (a quantity, a size)
	budgetMax = 100_000_000 // above this it is probably a typo or a phone number
)

var thaiDigits = strings.NewReplacer("๐", "0", "๑", "1", "๒", "2", "๓", "3", "๔", "4", "๕", "5", "๖", "6", "๗", "7", "๘", "8", "๙", "9")

func ExtractBudget(text string) (float64, bool) {
	text = thaiDigits.Replace(text)
	m := budgetRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "พัน", "k":
		v *= 1_000
	case "หมื่น":
		v *= 10_000
	case "แสน":
		v *= 100_000
	case "ล้าน":
		v *= 1_000_000
	}
	if v < budgetMin || v > budgetMax {
		return 0, false
	}
	return v, true
}
