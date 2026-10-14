// Package moex reads native ISS trading history in its own provider namespace.
package moex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type Listing struct{ SecurityID, ISIN, Board string }

func (l Listing) ID() string {
	return l.id("shares")
}

func (l Listing) BondID() string { return l.id("bonds") }
func (l Listing) id(market string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://iss.moex.com/stock/"+market+"/"+l.Board+"/"+l.SecurityID+"/"+l.ISIN)).String()
}

type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

type History struct {
	Listing         Listing
	Market          string
	From, To        string
	RetrievedAt     time.Time
	ResponseHashes  []string
	Observations    []md.Observation
	MissingSessions []string
}

type table struct {
	Columns []string            `json:"columns"`
	Data    [][]json.RawMessage `json:"data"`
}

func (t table) cell(row []json.RawMessage, column string) (string, error) {
	index := -1
	for i, c := range t.Columns {
		if c == column {
			if index >= 0 {
				return "", errors.New("duplicate ISS column")
			}
			index = i
		}
	}
	if index < 0 || len(row) != len(t.Columns) {
		return "", errors.New("missing ISS column or cell")
	}
	raw := row[index]
	if string(raw) == "null" {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return "", errors.New("invalid ISS cell")
	}
	return number.String(), nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (map[string]table, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://iss.moex.com/iss/"+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, "", errors.New("invalid ISS request")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", errors.New("ISS request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("ISS HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, "", errors.New("invalid or oversized ISS response")
	}
	var blocks map[string]table
	if json.Unmarshal(body, &blocks) != nil {
		return nil, "", errors.New("invalid ISS JSON")
	}
	hash := sha256.Sum256(body)
	return blocks, hex.EncodeToString(hash[:]), nil
}

var securityID = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{0,31}$`)
var isin = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{9}[0-9]$`)
var bondBoard = regexp.MustCompile(`^TQ[A-Z0-9]{2,6}$`)

// History loads actual OHLC trading summaries, not an adjusted candle series.
// Listing periods must already be reviewed: current ISIN metadata alone is not
// proof of historical ticker continuity. Bonds require their own quote/nominal
// contract and are deliberately outside this shares/funds adapter.
func (c *Client) History(ctx context.Context, listing Listing, from, to string) (History, error) {
	return c.history(ctx, listing, from, to, "shares")
}

// BondHistory retains the actual percentage quote and that day's FACEVALUE /
// FACEUNIT together. CURRENCYID is settlement currency, which may differ from
// the contractual nominal currency. No present-day nominal or NKD is substituted.
func (c *Client) BondHistory(ctx context.Context, listing Listing, from, to string) (History, error) {
	return c.history(ctx, listing, from, to, "bonds")
}

func (c *Client) history(ctx context.Context, listing Listing, from, to, market string) (History, error) {
	out := History{Listing: listing, Market: market, From: from, To: to, RetrievedAt: time.Now().UTC()}
	first, e1 := time.Parse(time.DateOnly, from)
	last, e2 := time.Parse(time.DateOnly, to)
	validBoard := market == "shares" && (listing.Board == "TQBR" || listing.Board == "TQTF") || market == "bonds" && bondBoard.MatchString(listing.Board)
	if !securityID.MatchString(listing.SecurityID) || !isin.MatchString(listing.ISIN) || !validBoard || e1 != nil || e2 != nil || first.Year() < 1900 || last.Before(first) || !last.Before(out.RetrievedAt.Truncate(24*time.Hour)) || last.Sub(first) > 3660*24*time.Hour {
		return out, errors.New("invalid ISS listing or completed date range")
	}
	blocks, hash, err := c.get(ctx, "securities/"+listing.SecurityID+".json", url.Values{"iss.meta": {"off"}, "iss.only": {"description"}})
	if err != nil {
		return out, err
	}
	desc := blocks["description"]
	identity := map[string]string{}
	for _, row := range desc.Data {
		name, e1 := desc.cell(row, "name")
		value, e2 := desc.cell(row, "value")
		if e1 != nil || e2 != nil {
			return out, errors.New("invalid ISS description")
		}
		if name == "SECID" || name == "ISIN" {
			if _, exists := identity[name]; exists {
				return out, errors.New("duplicate ISS identity")
			}
			identity[name] = value
		}
	}
	if identity["SECID"] != listing.SecurityID || identity["ISIN"] != listing.ISIN {
		return out, errors.New("ISS listing identity mismatch")
	}
	out.ResponseHashes = append(out.ResponseHashes, hash)
	seen := map[string]bool{}
	requestedColumns := "BOARDID,TRADEDATE,SECID,OPEN,HIGH,LOW,CLOSE,VOLUME,CURRENCYID"
	if market == "bonds" {
		requestedColumns += ",FACEVALUE,FACEUNIT"
	}
	for start, pages := 0, 0; pages < 50; pages++ {
		blocks, hash, err = c.get(ctx, "history/engines/stock/markets/"+market+"/boards/"+listing.Board+"/securities/"+listing.SecurityID+".json", url.Values{"from": {from}, "till": {to}, "start": {strconv.Itoa(start)}, "iss.meta": {"off"}, "history.columns": {requestedColumns}})
		if err != nil {
			return out, err
		}
		out.ResponseHashes = append(out.ResponseHashes, hash)
		history, cursor := blocks["history"], blocks["history.cursor"]
		if len(cursor.Data) != 1 {
			return out, errors.New("missing ISS pagination")
		}
		indexText, e1 := cursor.cell(cursor.Data[0], "INDEX")
		totalText, e2 := cursor.cell(cursor.Data[0], "TOTAL")
		index, e3 := strconv.Atoi(indexText)
		total, e4 := strconv.Atoi(totalText)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || index != start || total < start || total > 3661 || len(history.Data) > 1000 || start+len(history.Data) > total {
			return out, errors.New("invalid ISS pagination")
		}
		for _, row := range history.Data {
			values := map[string]string{}
			for _, column := range []string{"BOARDID", "TRADEDATE", "SECID", "OPEN", "HIGH", "LOW", "CLOSE", "VOLUME", "CURRENCYID"} {
				v, err := history.cell(row, column)
				if err != nil {
					return out, err
				}
				values[column] = v
			}
			day, err := time.Parse(time.DateOnly, values["TRADEDATE"])
			currency := nativeCurrency(values["CURRENCYID"])
			if err != nil || day.Before(first) || day.After(last) || seen[values["TRADEDATE"]] || values["SECID"] != listing.SecurityID || values["BOARDID"] != listing.Board || currency == "" || market == "shares" && currency != "RUB" {
				return out, errors.New("ISS history identity/date/currency mismatch")
			}
			seen[values["TRADEDATE"]] = true
			if values["CLOSE"] == "" || values["OPEN"] == "" || values["HIGH"] == "" || values["LOW"] == "" || values["VOLUME"] == "" {
				out.MissingSessions = append(out.MissingSessions, values["TRADEDATE"])
				continue
			}
			input := md.Observation{InstrumentID: listing.id(market), Kind: "daily_candle", At: day, SourceKind: "exchange", Currency: currency, PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: values["CLOSE"], Open: values["OPEN"], High: values["HIGH"], Low: values["LOW"], Volume: values["VOLUME"], Complete: true}
			if market == "bonds" {
				face, e1 := history.cell(row, "FACEVALUE")
				unit, e2 := history.cell(row, "FACEUNIT")
				if e1 != nil || e2 != nil {
					return out, errors.New("missing ISS bond nominal columns")
				}
				if face == "" || nativeCurrency(unit) == "" {
					out.MissingSessions = append(out.MissingSessions, values["TRADEDATE"])
					continue
				}
				input.PriceUnit, input.Nominal, input.NominalCurrency = "percent_of_nominal", face, nativeCurrency(unit)
				input.Currency = input.NominalCurrency
			}
			o, err := md.Normalize(input)
			if err != nil {
				return out, errors.New("invalid ISS OHLC or volume")
			}
			for _, value := range []string{o.Price, o.Open, o.High, o.Low} {
				amount, ok := new(big.Rat).SetString(value)
				if !ok || amount.Sign() <= 0 {
					return out, errors.New("nonpositive ISS equity price")
				}
			}
			out.Observations = append(out.Observations, o)
		}
		start += len(history.Data)
		if start == total {
			sort.Slice(out.Observations, func(i, j int) bool { return out.Observations[i].At.Before(out.Observations[j].At) })
			return out, nil
		}
		if len(history.Data) == 0 {
			return out, errors.New("ISS pagination made no progress")
		}
	}
	return out, errors.New("ISS history page limit exceeded")
}

func nativeCurrency(value string) string {
	switch value {
	case "SUR", "RUB":
		return "RUB"
	case "USD", "EUR", "CNY":
		return value
	default:
		return ""
	}
}
