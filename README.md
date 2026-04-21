# hibachi-cli

Unofficial command-line client for the Hibachi perpetual futures exchange. Not affiliated with Hibachi.

Built on [hibachi-go](https://github.com/doedja/hibachi-go).

## What it does

- Wraps the full Hibachi SDK: market data, trading, capital, websockets.
- Natural-language trading via Claude. Type what you want, get a plan, approve, fire.
- Two AI backends you can switch between: local `claude` CLI, or OpenRouter with any model.
- Persistent markdown memory so the AI gets smarter about how you trade.
- SQLite journal of every plan, fill, and advisor run.

## Install

```
go install github.com/doedja/hibachi-cli@latest
```

## Quick start

```
hibachi auth login
hibachi market price BTC/USDT-P
hibachi buy btc 100 usd at 72000
```

## Sign up

No Hibachi account yet? Sign up at [hibachi.xyz/r/hoshii](https://hibachi.xyz/r/hoshii).

## Status

Early. v0.1 in development. APIs may change.

## License

MIT.
