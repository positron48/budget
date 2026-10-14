package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

// ObservationBondFacts exposes only the session nominal carried by the same
// immutable selected observation revision. It supplies no coupon entitlement,
// accrued interest or other missing bond contract information.
func ObservationBondFacts(saved SavedObservation) (*SavedBondFacts, error) {
	if saved.Nominal == "" && saved.NominalCurrency == "" {
		return nil, nil
	}
	o, err := Normalize(saved.Observation)
	if err != nil || saved.ID == "" || saved.Revision < 1 || saved.ReceivedAt.IsZero() || saved.ReceivedAt.Before(o.At) || !o.At.Equal(o.At.Truncate(24*time.Hour)) {
		return nil, ErrInvalid
	}
	nominal := o.Nominal
	facts := BondFacts{Instrument: Instrument{Detailed: true, ID: o.InstrumentID, Ticker: o.InstrumentID, Name: "Native session nominal", Kind: "bond", Currency: o.NominalCurrency}, From: o.At, To: o.At.Add(24*time.Hour - time.Second), AsOf: saved.ReceivedAt, Terms: BondTerms{Nominal: &BondAmount{Amount: nominal, Currency: o.NominalCurrency}}, Accrued: []BondAccrued{{Date: o.At, Nominal: &nominal}}}
	facts, err = NormalizeBondFacts(facts)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	return &SavedBondFacts{BondFacts: facts, ID: saved.ID + ":" + strconv.FormatInt(saved.Revision, 10), Hash: hex.EncodeToString(hash[:]), RecordedAt: saved.ReceivedAt}, nil
}
