package bepusdt

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveGateway is opt-in and creates, queries, then cancels a real pending
// BEpusdt order. It never performs a blockchain transfer.
func TestLiveGateway(t *testing.T) {
	baseURL := os.Getenv("BEPUSDT_BASE_URL")
	token := os.Getenv("BEPUSDT_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set BEPUSDT_BASE_URL and BEPUSDT_TOKEN to run the live gateway test")
	}
	amount := 10.0
	if raw := os.Getenv("BEPUSDT_TEST_AMOUNT"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid BEPUSDT_TEST_AMOUNT")
		}
		amount = parsed
	}
	client, err := New(Config{BaseURL: baseURL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	orderID := "sub2api-live-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	result, err := client.Create(ctx, CreateRequest{
		OrderID: orderID, Amount: amount, Fiat: "CNY", Name: "Sub2API integration test",
		NotifyURL:   "https://example.com/sub2api-bepusdt-test-notify",
		RedirectURL: "https://example.com/sub2api-bepusdt-test-return",
		TradeType:   "usdt.trc20", Timeout: 180,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.Cancel(cleanupCtx, result.TradeID)
	})
	order, err := client.Query(ctx, result.TradeID)
	if err != nil {
		t.Fatal(err)
	}
	if order.OrderID != orderID || order.Status != StatusWaiting {
		t.Fatalf("unexpected live order: order_id=%q status=%d", order.OrderID, order.Status)
	}
	if err := client.Cancel(ctx, result.TradeID); err != nil {
		t.Fatal(err)
	}
	order, err = client.Query(ctx, result.TradeID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != StatusCanceled {
		t.Fatalf("cancelled order status=%d, want %d", order.Status, StatusCanceled)
	}
}
