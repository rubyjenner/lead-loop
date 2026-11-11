package main

import "testing"

func TestValidateRetention(t *testing.T) {
	ok := []retentionCfg{{0, 0}, {30, 0}, {365, 30}, {3650, 3650}, {0, 7}}
	bad := []retentionCfg{{1, 0}, {29, 0}, {3651, 0}, {-5, 0}, {0, 1}, {0, 6}, {0, 3651}, {0, -1}}
	for _, c := range ok {
		if err := validateRetention(c); err != nil {
			t.Errorf("%+v should be valid: %v", c, err)
		}
	}
	for _, c := range bad {
		if validateRetention(c) == nil {
			t.Errorf("%+v should be rejected (a typo must never delete everything)", c)
		}
	}
}
