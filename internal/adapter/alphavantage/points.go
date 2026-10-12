package alphavantage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// RetrospectivePoints converts a complete, validated checkpoint under an explicit
// application convention. It never infers publication time from LastRefreshed.
// Both response and normalized checkpoint hashes are retained in the locator.
func RetrospectivePoints(p md.FXPolicy, a Audit) ([]md.FXPoint, error) {
	if p.Alpha == nil || md.ValidateFXPolicy(p) != nil || (a.Pair != "USD/RUB" && a.Pair != "EUR/RUB") {
		return nil, md.ErrInvalid
	}
	if _, err := a.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	checkpoint := hex.EncodeToString(sum[:])
	parts := strings.Split(a.Pair, "/")
	points := make([]md.FXPoint, 0, len(a.Bars))
	for _, b := range a.Bars {
		day, err := time.Parse(time.DateOnly, b.Date)
		if err != nil || day.Before(p.Alpha.From) {
			return nil, md.ErrInvalid
		}
		at := day.Add(24 * time.Hour)
		if at.After(a.RetrievedAt) {
			return nil, md.ErrInvalid
		}
		point, err := md.NormalizeFXPoint(md.FXPoint{From: parts[0], To: parts[1], Rate: b.Close, At: at, AvailableAt: at, ValidUntil: at.Add(96 * time.Hour), EvidenceSHA256: checkpoint, RecordReference: fmt.Sprintf("alphavantage/FX_DAILY/%s/%s/close;response_sha256=%s;convention=%s", a.Pair, b.Date, a.BodySHA256, md.AlphaRetrospectiveConvention)})
		if err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, nil
}
