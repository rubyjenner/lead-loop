package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFollowupAllowance(t *testing.T) {
	cases := []struct {
		q                   lineQuota
		reserve, perTick, w int
	}{
		{lineQuota{Limit: 200, Used: 0}, 20, 5, 5},
		{lineQuota{Limit: 200, Used: 178}, 20, 5, 2}, // 22 left, keep 20
		{lineQuota{Limit: 200, Used: 180}, 20, 5, 0}, // only the reserve is left
		{lineQuota{Limit: 200, Used: 250}, 20, 5, 0}, // over
		{lineQuota{Unlimited: true}, 20, 5, 5},
	}
	for _, c := range cases {
		if got := followupAllowance(c.q, c.reserve, c.perTick); got != c.w {
			t.Errorf("%+v: got %d want %d", c.q, got, c.w)
		}
	}
}

func TestLineQuotaFromAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/bot/message/quota":
			_, _ = w.Write([]byte(`{"type":"limited","value":200}`))
		case "/v2/bot/message/quota/consumption":
			_, _ = w.Write([]byte(`{"totalUsage":37}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := &lineClient{token: "tok", http: &http.Client{Transport: rewriteTransport{srv.URL}}}
	q, err := c.quota(context.Background())
	if err != nil || q.Limit != 200 || q.Used != 37 || q.Unlimited || q.remaining() != 163 {
		t.Fatalf("got %+v err=%v", q, err)
	}
}
