package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/user"
	"time"

	"github.com/positron48/budget/internal/adapter/moex"
	"github.com/positron48/budget/internal/adapter/nativehistory"
	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/adapter/tinvest"
	"github.com/positron48/budget/internal/adapter/twelvedata"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func workerProvider(ctx context.Context, repo *postgres.MarketDataRepo, scope md.Scope) (md.Provider, error) {
	lookup := func(ctx context.Context, id string) (md.Instrument, error) {
		return repo.NativeMarketInstrument(ctx, scope, id)
	}
	switch scope.Provider {
	case "moex":
		client := moex.New()
		hash := sha256.Sum256([]byte("moex:public-iss"))
		key := hex.EncodeToString(hash[:])
		client.SetRequestGate(func(ctx context.Context) error { return repo.WaitMarketPermit(ctx, key, 600*time.Millisecond) })
		return &nativehistory.Provider{Scope: scope, Lookup: lookup, MOEX: client}, nil
	case "twelvedata":
		var client *twelvedata.Client
		var e error
		if path := os.Getenv("TWELVEDATA_KEY_FILE"); path != "" {
			client, e = twelvedata.NewFromSecretFile(path)
		} else {
			var u *user.User
			u, e = user.Current()
			if e == nil {
				client, e = twelvedata.NewFromKeychain(ctx, u.Username)
			}
		}
		if e != nil {
			return nil, errors.New("twelve data credential unavailable; configure Keychain or TWELVEDATA_KEY_FILE")
		}
		client.SetRequestGate(repo.WaitNativeEquityPermit)
		client.SetRequestBackoff(repo.DeferMarketPermit)
		return &nativehistory.Provider{Scope: scope, Lookup: lookup, Equities: client}, nil
	case "tinvest":
		var client *tinvest.Client
		var e error
		if path := os.Getenv("TINVEST_TOKEN_FILE"); path != "" {
			client, e = tinvest.NewFromSecretFile(path)
		} else {
			var u *user.User
			u, e = user.Current()
			if e == nil {
				client, e = tinvest.NewFromKeychain(ctx, u.Username)
			}
		}
		if e != nil {
			return nil, errors.New("T-Invest credential unavailable; configure Keychain or TINVEST_TOKEN_FILE")
		}
		client.SetRequestGate(func(ctx context.Context, key string) error {
			return repo.WaitMarketPermit(ctx, key, 600*time.Millisecond)
		})
		client.SetRequestBackoff(repo.DeferMarketPermit)
		return client, nil
	default:
		return nil, md.ErrInvalid
	}
}
