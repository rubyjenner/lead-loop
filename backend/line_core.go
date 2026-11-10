package main

// LINE Messaging API building blocks. Standard library only (no HTTP framework, no database), so this file
// can be tested on its own. The HTTP route and the database work are in line.go.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	lineChannel     = "line"
	lineReplyURL    = "https://api.line.me/v2/bot/message/reply"
	lineProfileURL  = "https://api.line.me/v2/bot/profile/"
	lineMaxText     = 5000 // LINE's limit for one text message
	lineWithdrawCmd = "ถอนความยินยอม"
)

// LINE user ids are "U" + 32 lowercase hex characters. Anything else is refused before it is stored or put in a URL.
var lineUserIDRe = regexp.MustCompile(`^U[0-9a-f]{32}$`)

// lineSignatureValid checks X-Line-Signature: base64(HMAC-SHA256(channel secret, raw body)).
func lineSignatureValid(secret string, body []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

type lineSource struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
}

type lineEvent struct {
	Type       string     `json:"type"`
	ReplyToken string     `json:"replyToken"`
	Source     lineSource `json:"source"`
	Message    struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"message"`
	Postback struct {
		Data string `json:"data"`
	} `json:"postback"`
}

type lineWebhook struct {
	Events []lineEvent `json:"events"`
}

// linePostbackAction reads "action=consent" style data from a button.
func linePostbackAction(data string) string {
	v, err := url.ParseQuery(data)
	if err != nil {
		return ""
	}
	return v.Get("action")
}

// ---- outgoing messages ----

type lineMessage map[string]any

func lineTruncate(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

func lineQuick(items ...map[string]any) map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		out = append(out, map[string]any{"type": "action", "action": a})
	}
	return map[string]any{"items": out}
}

// lineText is a plain reply with a "withdraw consent" shortcut button (PDPA: easy to withdraw).
func lineText(text string) lineMessage {
	return lineMessage{
		"type": "text", "text": lineTruncate(text, lineMaxText),
		"quickReply": lineQuick(map[string]any{"type": "message", "label": lineWithdrawCmd, "text": lineWithdrawCmd}),
	}
}

// linePlain is a reply with no buttons.
func linePlain(text string) lineMessage {
	return lineMessage{"type": "text", "text": lineTruncate(text, lineMaxText)}
}

// lineConsentMessage shows the consent notice with two buttons. Nothing is stored until "ยินยอม" is pressed.
func lineConsentMessage(text string) lineMessage {
	return lineMessage{
		"type": "text", "text": lineTruncate(text, lineMaxText),
		"quickReply": lineQuick(
			map[string]any{"type": "postback", "label": "ยินยอม", "data": "action=consent", "displayText": "ยินยอม"},
			map[string]any{"type": "postback", "label": "ไม่ยินยอม", "data": "action=decline", "displayText": "ไม่ยินยอม"},
		),
	}
}

// ---- LINE API client ----

type lineClient struct {
	token string
	http  *http.Client
}

func (c *lineClient) do(ctx context.Context, method, endpoint string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("LINE API %s returned %d: %s", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// reply sends up to 5 messages. A reply token works once and only for a short time.
func (c *lineClient) reply(ctx context.Context, replyToken string, msgs ...lineMessage) error {
	if replyToken == "" || len(msgs) == 0 {
		return errors.New("no reply token or message")
	}
	_, err := c.do(ctx, http.MethodPost, lineReplyURL, map[string]any{"replyToken": replyToken, "messages": msgs})
	return err
}

// linePushURL is a variable only so a test can point it at a local server.
var linePushURL = "https://api.line.me/v2/bot/message/push"

// push sends a message to one person without a reply token (used when a salesperson answers from the lead page).
// The caller must already have checked that the person has consented and has not withdrawn.
// LINE counts push messages against the account's monthly quota.
func (c *lineClient) push(ctx context.Context, userID string, msgs ...lineMessage) error {
	if !lineUserIDRe.MatchString(userID) {
		return errors.New("invalid LINE user id")
	}
	if len(msgs) == 0 {
		return errors.New("no message")
	}
	_, err := c.do(ctx, http.MethodPost, linePushURL, map[string]any{"to": userID, "messages": msgs})
	return err
}

// displayName returns the person's LINE display name, or "" if it cannot be read.
// Call it only AFTER the person has accepted the consent notice.
func (c *lineClient) displayName(ctx context.Context, userID string) string {
	if !lineUserIDRe.MatchString(userID) {
		return ""
	}
	data, err := c.do(ctx, http.MethodGet, lineProfileURL+userID, nil)
	if err != nil {
		return ""
	}
	var p struct {
		DisplayName string `json:"displayName"`
	}
	if json.Unmarshal(data, &p) != nil {
		return ""
	}
	return p.DisplayName
}
