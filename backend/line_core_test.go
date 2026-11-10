package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func lineSign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func TestLineSignature(t *testing.T) {
	body := []byte(`{"events":[]}`)
	good := lineSign("secret", body)
	if !lineSignatureValid("secret", body, good) {
		t.Fatal("valid signature rejected")
	}
	for name, c := range map[string]struct{ secret, sig string }{
		"wrong secret":  {"other", good},
		"empty header":  {"secret", ""},
		"empty secret":  {"", good},
		"not base64":    {"secret", "%%%"},
		"wrong content": {"secret", lineSign("secret", []byte("x"))},
	} {
		if lineSignatureValid(c.secret, body, c.sig) {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLineUserID(t *testing.T) {
	ok := "U" + strings.Repeat("a1", 16)
	if !lineUserIDRe.MatchString(ok) {
		t.Fatal("valid id rejected")
	}
	for _, bad := range []string{"", "U123", "u" + strings.Repeat("a1", 16), ok + "x", "U" + strings.Repeat("G", 32), "../" + ok} {
		if lineUserIDRe.MatchString(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestLineParseEvents(t *testing.T) {
	raw := `{"destination":"x","events":[
	  {"type":"message","replyToken":"rt1","source":{"type":"user","userId":"Uabc"},"message":{"type":"text","text":"ราคาเท่าไหร่"}},
	  {"type":"postback","replyToken":"rt2","source":{"type":"user","userId":"Uabc"},"postback":{"data":"action=consent"}}]}`
	var wh lineWebhook
	if err := json.Unmarshal([]byte(raw), &wh); err != nil || len(wh.Events) != 2 {
		t.Fatalf("parse: %v %+v", err, wh)
	}
	if wh.Events[0].Message.Text != "ราคาเท่าไหร่" || wh.Events[0].ReplyToken != "rt1" {
		t.Errorf("message event: %+v", wh.Events[0])
	}
	if linePostbackAction(wh.Events[1].Postback.Data) != "consent" {
		t.Errorf("postback action: %q", linePostbackAction(wh.Events[1].Postback.Data))
	}
	if linePostbackAction("%zz") != "" || linePostbackAction("") != "" {
		t.Error("bad postback data must give empty action")
	}
}

func TestLineMessages(t *testing.T) {
	m := lineConsentMessage("ยินยอมไหม")
	b, _ := json.Marshal(m)
	s := string(b)
	for _, want := range []string{`"type":"text"`, `"action=consent"`, `"action=decline"`, `"quickReply"`} {
		if !strings.Contains(s, want) {
			t.Errorf("consent message missing %s: %s", want, s)
		}
	}
	if strings.Contains(s, "score") || strings.Contains(s, "lead") {
		t.Errorf("consent message leaks internals: %s", s)
	}
	long := lineText(strings.Repeat("ก", lineMaxText+50))
	if n := len([]rune(long["text"].(string))); n != lineMaxText {
		t.Errorf("text not truncated: %d", n)
	}
	w, _ := json.Marshal(lineText("hi"))
	if !strings.Contains(string(w), lineWithdrawCmd) {
		t.Error("normal replies must carry the withdraw button")
	}
	p, _ := json.Marshal(linePlain("hi"))
	if strings.Contains(string(p), "quickReply") {
		t.Error("plain reply must have no buttons")
	}
}

type rewriteTransport struct{ target string }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme, u.Host = "http", strings.TrimPrefix(r.target, "http://")
	req2 := req.Clone(req.Context())
	req2.URL = &u
	return http.DefaultTransport.RoundTrip(req2)
}

func TestLineClient(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			if gotBody["replyToken"] == "bad" {
				http.Error(w, `{"message":"Invalid reply token"}`, 400)
				return
			}
		}
		_, _ = w.Write([]byte(`{"displayName":"สมชาย"}`))
	}))
	defer srv.Close()
	c := &lineClient{token: "tok", http: &http.Client{Transport: rewriteTransport{srv.URL}}}
	ctx := context.Background()

	if err := c.reply(ctx, "rt", linePlain("hello")); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotPath != "/v2/bot/message/reply" || gotBody["replyToken"] != "rt" {
		t.Errorf("reply request: %s %s %v", gotAuth, gotPath, gotBody)
	}
	if err := c.reply(ctx, "bad", linePlain("x")); err == nil {
		t.Error("a 400 from LINE must be an error")
	}
	if err := c.reply(ctx, "", linePlain("x")); err == nil {
		t.Error("empty reply token must be an error")
	}
	id := "U" + strings.Repeat("0f", 16)
	if n := c.displayName(ctx, id); n != "สมชาย" || gotPath != "/v2/bot/profile/"+id {
		t.Errorf("profile: %q %s", n, gotPath)
	}
	if c.displayName(ctx, "../../etc") != "" {
		t.Error("invalid user id must not reach the network")
	}
}

func TestLinePush(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		if gotBody["to"] == "U"+strings.Repeat("bb", 16) {
			http.Error(w, `{"message":"blocked"}`, 400)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &lineClient{token: "tok", http: &http.Client{Transport: rewriteTransport{srv.URL}}}
	ctx := context.Background()
	id := "U" + strings.Repeat("aa", 16)

	if err := c.push(ctx, id, lineText("สวัสดีค่ะ")); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotPath != "/v2/bot/message/push" || gotBody["to"] != id {
		t.Errorf("push request: %s %s %v", gotAuth, gotPath, gotBody)
	}
	if msgs, _ := gotBody["messages"].([]any); len(msgs) != 1 {
		t.Errorf("messages: %v", gotBody["messages"])
	}
	if err := c.push(ctx, "U"+strings.Repeat("bb", 16), lineText("x")); err == nil {
		t.Error("a 400 from LINE (for example a blocked account) must be an error")
	}
	before := calls
	if err := c.push(ctx, "../../etc", lineText("x")); err == nil || calls != before {
		t.Error("invalid user id must be refused before the network")
	}
	if err := c.push(ctx, id); err == nil || calls != before {
		t.Error("no message must be refused before the network")
	}
}
