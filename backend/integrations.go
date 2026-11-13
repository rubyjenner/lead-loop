package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var lineCredentialCache = struct {
	sync.RWMutex
	secret string
	token  string
}{}

func integrationKey() []byte {
	v := os.Getenv("INTEGRATION_ENCRYPTION_KEY")
	if len(v) != 32 {
		return nil
	}
	return []byte(v)
}

func encryptIntegrationValue(plain string) ([]byte, error) {
	key := integrationKey()
	if len(key) != 32 {
		return nil, errors.New("INTEGRATION_ENCRYPTION_KEY must be exactly 32 characters")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}

func decryptIntegrationValue(ciphertext []byte) (string, error) {
	key := integrationKey()
	if len(key) != 32 {
		return "", errors.New("integration encryption key is not configured")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted integration value")
	}
	nonce, data := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, nil)
	return string(plain), err
}

func initIntegrationStore(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS integration_credentials (
		workspace_key TEXT PRIMARY KEY DEFAULT 'default', provider TEXT NOT NULL, account_name TEXT NOT NULL DEFAULT '',
		channel_secret BYTEA NOT NULL, access_token BYTEA NOT NULL, bot_name TEXT NOT NULL DEFAULT '',
		bot_user_id TEXT NOT NULL DEFAULT '', verified_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_by TEXT NOT NULL DEFAULT '')`); err != nil {
		return err
	}
	if len(integrationKey()) == 32 {
		var secretCipher, tokenCipher []byte
		err := pool.QueryRow(ctx, `SELECT channel_secret,access_token FROM integration_credentials WHERE workspace_key='default' AND provider='line'`).Scan(&secretCipher, &tokenCipher)
		if err == nil {
			secret, e1 := decryptIntegrationValue(secretCipher)
			token, e2 := decryptIntegrationValue(tokenCipher)
			if e1 != nil || e2 != nil {
				return errors.New("could not decrypt stored LINE credentials; check INTEGRATION_ENCRYPTION_KEY")
			}
			lineCredentialCache.Lock()
			lineCredentialCache.secret = secret
			lineCredentialCache.token = token
			lineCredentialCache.Unlock()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

func setStoredLineCredentials(secret, token string) {
	lineCredentialCache.Lock()
	lineCredentialCache.secret = secret
	lineCredentialCache.token = token
	lineCredentialCache.Unlock()
}

func storedLineCredentials() (string, string) {
	lineCredentialCache.RLock()
	defer lineCredentialCache.RUnlock()
	return lineCredentialCache.secret, lineCredentialCache.token
}

func registerIntegrationRoutes(app fiber.Router, pool *pgxpool.Pool) {
	admin := requireRole("admin")
	app.Get("/integrations/status", admin, func(c *fiber.Ctx) error {
		var account, botName, botUser string
		var verified *time.Time
		err := pool.QueryRow(c.Context(), `SELECT account_name,bot_name,bot_user_id,verified_at FROM integration_credentials WHERE workspace_key='default' AND provider='line'`).Scan(&account, &botName, &botUser, &verified)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return serverError(c, err)
		}
		secret, token := storedLineCredentials()
		if secret == "" {
			secret = strings.TrimSpace(os.Getenv("LINE_CHANNEL_SECRET"))
		}
		if token == "" {
			token = strings.TrimSpace(os.Getenv("LINE_CHANNEL_ACCESS_TOKEN"))
		}
		return c.JSON(fiber.Map{"line": fiber.Map{"enabled": secret != "" && token != "", "secret_configured": secret != "", "token_configured": token != "", "account_name": account, "bot_name": botName, "bot_user_id": botUser, "verified": verified != nil, "verified_at": verified, "webhook_path": "/api/public/line/webhook"}, "web_chat": fiber.Map{"enabled": strings.TrimSpace(os.Getenv("PUBLIC_CHAT_ENABLED")) == "1"}, "encryption_ready": len(integrationKey()) == 32})
	})
	app.Post("/integrations/line/connect", admin, func(c *fiber.Ctx) error {
		if len(integrationKey()) != 32 {
			return c.Status(503).JSON(fiber.Map{"error": "server encryption key is not configured; ask the system administrator"})
		}
		var in struct {
			AccountName   string `json:"account_name"`
			ChannelSecret string `json:"channel_secret"`
			AccessToken   string `json:"access_token"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
		}
		in.AccountName = strings.TrimSpace(in.AccountName)
		in.ChannelSecret = strings.TrimSpace(in.ChannelSecret)
		in.AccessToken = strings.TrimSpace(in.AccessToken)
		if in.AccountName == "" || in.ChannelSecret == "" || in.AccessToken == "" {
			return c.Status(400).JSON(fiber.Map{"error": "account name, channel secret and access token are required"})
		}
		client := &lineClient{token: in.AccessToken, http: &http.Client{Timeout: 8 * time.Second}}
		data, err := client.do(c.UserContext(), http.MethodGet, "https://api.line.me/v2/bot/info", nil)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "LINE verification failed; check the Channel Access Token"})
		}
		var bot struct {
			DisplayName string `json:"displayName"`
			UserID      string `json:"userId"`
		}
		if err := jsonUnmarshal(data, &bot); err != nil {
			return c.Status(502).JSON(fiber.Map{"error": "LINE returned an invalid bot profile"})
		}
		secretCipher, err := encryptIntegrationValue(in.ChannelSecret)
		if err != nil {
			return serverError(c, err)
		}
		tokenCipher, err := encryptIntegrationValue(in.AccessToken)
		if err != nil {
			return serverError(c, err)
		}
		_, err = pool.Exec(c.Context(), `INSERT INTO integration_credentials(workspace_key,provider,account_name,channel_secret,access_token,bot_name,bot_user_id,verified_at,updated_at,updated_by)
			VALUES('default','line',$1,$2,$3,$4,$5,now(),now(),$6) ON CONFLICT(workspace_key) DO UPDATE SET provider='line',account_name=EXCLUDED.account_name,channel_secret=EXCLUDED.channel_secret,access_token=EXCLUDED.access_token,bot_name=EXCLUDED.bot_name,bot_user_id=EXCLUDED.bot_user_id,verified_at=now(),updated_at=now(),updated_by=EXCLUDED.updated_by`, in.AccountName, secretCipher, tokenCipher, bot.DisplayName, bot.UserID, currentUser(c).Email)
		if err != nil {
			return serverError(c, err)
		}
		setStoredLineCredentials(in.ChannelSecret, in.AccessToken)
		auditUser(c, pool, "line_integration_connected", "integration:line", "LINE credentials verified and encrypted")
		return c.JSON(fiber.Map{"ok": true, "account_name": in.AccountName, "bot_name": bot.DisplayName, "verified": true})
	})
	app.Delete("/integrations/line", admin, func(c *fiber.Ctx) error {
		if _, err := pool.Exec(c.Context(), `DELETE FROM integration_credentials WHERE workspace_key='default' AND provider='line'`); err != nil {
			return serverError(c, err)
		}
		setStoredLineCredentials("", "")
		auditUser(c, pool, "line_integration_disconnected", "integration:line", "")
		return c.JSON(fiber.Map{"ok": true})
	})
}

// tiny wrapper keeps the integration handler independent of the LINE webhook's request structs.
func jsonUnmarshal(data []byte, out any) error { return json.Unmarshal(data, out) }
