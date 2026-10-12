package investmentimport

import (
	"fmt"
	"github.com/google/uuid"
	"regexp"
	"strings"
)

// InstrumentIdentity is an explicit reviewed mapping, never inferred by ticker.
type InstrumentIdentity struct {
	FIGI        string `json:"figi"`
	PositionUID string `json:"position_uid"`
	Reason      string `json:"reason"`
}

var evidenceFIGI = regexp.MustCompile(`^[A-Z0-9]{12}$`)

func (m FinancialMapping) instrumentKey(uid, figi, position string) (string, error) {
	identity, ok := m.InstrumentIdentities[uid]
	if !ok {
		return "tinvest:" + uid, nil
	}
	parsed, err := uuid.Parse(identity.PositionUID)
	if err != nil || parsed.String() != identity.PositionUID || !evidenceFIGI.MatchString(identity.FIGI) || identity.FIGI != figi || identity.PositionUID != position || strings.TrimSpace(identity.Reason) == "" || len(identity.Reason) > 2000 {
		return "", fmt.Errorf("broker_instrument_identity_mismatch")
	}
	return "tinvest-position:" + position, nil
}
