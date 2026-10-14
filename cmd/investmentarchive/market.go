package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/positron48/budget/internal/adapter/postgres"
)

func readReplacementMarket(path, expected string) (*postgres.PortfolioMarketInput, error) {
	if path == "" {
		return nil, nil
	}
	file, e := os.Open(path)
	if e != nil {
		return nil, errors.New("cannot read replacement market evidence")
	}
	defer func() { _ = file.Close() }()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16<<20 {
		return nil, errors.New("market evidence must be a private regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20+1))
	decoder.DisallowUnknownFields()
	var market postgres.PortfolioMarketInput
	if decoder.Decode(&market) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid replacement market evidence")
	}
	if postgres.PortfolioMarketSHA256(market) != expected {
		return nil, errors.New("replacement market checksum mismatch")
	}
	return &market, nil
}
