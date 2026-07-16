# BEpusdt Go client

This directory is an independent, dependency-free Go module used by Sub2API's
payment provider adapter. It talks to a separately deployed BEpusdt service; it
does not embed or start BEpusdt.

```go
client, err := bepusdt.New(bepusdt.Config{
    BaseURL: "https://pay.example.com",
    Token:   "your-bepusdt-api-token",
})
if err != nil {
    return err
}

result, err := client.Create(ctx, bepusdt.CreateRequest{
    OrderID:     "ORDER-001",
    Amount:      28.88,
    Fiat:        "CNY",
    NotifyURL:   "https://api.example.com/api/v1/payment/webhook/bepusdt",
    RedirectURL: "https://api.example.com/payment/result",
})
```

Leaving `TradeType` empty uses BEpusdt's hosted cashier. Set a value such as
`usdt.trc20` to create a fixed-network transaction.
