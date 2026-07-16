// Package bepusdt is a dependency-free client for a separately deployed
// BEpusdt payment gateway.
package bepusdt

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	CreateTransactionPath = "/api/v1/order/create-transaction"
	CreateOrderPath       = "/api/v1/order/create-order"
	CancelTransactionPath = "/api/v1/order/cancel-transaction"
	OrderInfoPath         = "/api/v1/pay/info"

	StatusWaiting    = 1
	StatusSuccess    = 2
	StatusExpired    = 3
	StatusCanceled   = 4
	StatusConfirming = 5
	StatusFailed     = 6

	defaultHTTPTimeout = 10 * time.Second
	maxResponseSize    = 1 << 20
)

type Config struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type CreateRequest struct {
	OrderID     string
	Amount      float64
	Fiat        string
	Name        string
	NotifyURL   string
	RedirectURL string
	TradeType   string
	Currencies  string
	Timeout     int
}

type CreateResult struct {
	TradeID    string `json:"trade_id"`
	OrderID    string `json:"order_id"`
	PaymentURL string `json:"payment_url"`
}

type Order struct {
	TradeID string `json:"trade_id"`
	OrderID string `json:"order_id"`
	Status  int    `json:"status"`
	Money   string `json:"money"`
	Fiat    string `json:"fiat"`
}

type Notification struct {
	TradeID            string  `json:"trade_id"`
	OrderID            string  `json:"order_id"`
	Amount             float64 `json:"amount"`
	ActualAmount       string  `json:"actual_amount"`
	Token              string  `json:"token"`
	BlockTransactionID string  `json:"block_transaction_id"`
	Signature          string  `json:"signature"`
	Status             int     `json:"status"`
}

type apiResponse struct {
	StatusCode int             `json:"status_code"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}

func New(config Config) (*Client, error) {
	baseURL, err := normalizeBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Token) == "" {
		return nil, fmt.Errorf("token is required")
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{baseURL: baseURL, token: config.Token, httpClient: httpClient}, nil
}

func normalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base URL must be an absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("base URL must use http or https")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.RawPath = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	for _, endpoint := range []string{CreateTransactionPath, CreateOrderPath, CancelTransactionPath, OrderInfoPath} {
		if strings.HasSuffix(strings.ToLower(parsed.Path), endpoint) {
			parsed.Path = strings.TrimRight(parsed.Path[:len(parsed.Path)-len(endpoint)], "/")
			break
		}
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *Client) Create(ctx context.Context, request CreateRequest) (*CreateResult, error) {
	payload := map[string]any{
		"order_id": request.OrderID, "amount": request.Amount, "fiat": request.Fiat,
		"name": request.Name, "notify_url": request.NotifyURL, "redirect_url": request.RedirectURL,
	}
	if request.Timeout > 0 {
		payload["timeout"] = request.Timeout
	}
	path := CreateOrderPath
	if strings.TrimSpace(request.TradeType) != "" {
		path = CreateTransactionPath
		payload["trade_type"] = strings.TrimSpace(request.TradeType)
	} else if strings.TrimSpace(request.Currencies) != "" {
		payload["currencies"] = strings.TrimSpace(request.Currencies)
	}
	payload["signature"] = Sign(payload, c.token)

	var response apiResponse
	if err := c.postJSON(ctx, path, payload, &response); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway error: %s", response.Message)
	}
	var result CreateResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}
	if strings.TrimSpace(result.TradeID) == "" || strings.TrimSpace(result.PaymentURL) == "" {
		return nil, fmt.Errorf("create response missing trade_id or payment_url")
	}
	return &result, nil
}

func (c *Client) Query(ctx context.Context, tradeID string) (*Order, error) {
	var response apiResponse
	if err := c.postJSON(ctx, OrderInfoPath, map[string]any{"trade_id": strings.TrimSpace(tradeID)}, &response); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway error: %s", response.Message)
	}
	var order Order
	if err := json.Unmarshal(response.Data, &order); err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}
	return &order, nil
}

func (c *Client) Cancel(ctx context.Context, tradeID string) error {
	payload := map[string]any{"trade_id": strings.TrimSpace(tradeID)}
	payload["signature"] = Sign(payload, c.token)
	var response apiResponse
	if err := c.postJSON(ctx, CancelTransactionPath, payload, &response); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway error: %s", response.Message)
	}
	return nil
}

func (c *Client) VerifyNotification(rawBody []byte) (*Notification, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, fmt.Errorf("decode notification: %w", err)
	}
	received, _ := payload["signature"].(string)
	if received == "" {
		return nil, fmt.Errorf("notification signature is missing")
	}
	expected := Sign(payload, c.token)
	if !hmac.Equal([]byte(strings.ToLower(received)), []byte(expected)) {
		return nil, fmt.Errorf("notification signature mismatch")
	}
	var notification Notification
	if err := json.Unmarshal(rawBody, &notification); err != nil {
		return nil, fmt.Errorf("decode notification fields: %w", err)
	}
	return &notification, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload map[string]any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return err
	}
	if len(responseBody) > maxResponseSize {
		return fmt.Errorf("response exceeds %d bytes", maxResponseSize)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// Sign implements BEpusdt's canonical signature algorithm.
func Sign(payload map[string]any, token string) string {
	keys := make([]string, 0, len(payload))
	for key, value := range payload {
		if key == "signature" || emptySignValue(value) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(payload[key]))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + token))
	return hex.EncodeToString(sum[:])
}

func emptySignValue(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && text == ""
}
