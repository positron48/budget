package tinvest

import (
	"context"
	"encoding/json"
	"fmt"
)

type Account struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	OpenedDate  string `json:"openedDate"`
	ClosedDate  string `json:"closedDate"`
	AccessLevel string `json:"accessLevel"`
}

func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	var r struct {
		Accounts []Account `json:"accounts"`
	}
	err := c.read(ctx, "UsersService/GetAccounts", map[string]any{}, &r)
	return r.Accounts, err
}

type OperationsPage struct {
	HasNext    bool              `json:"hasNext"`
	NextCursor string            `json:"nextCursor"`
	Items      []json.RawMessage `json:"items"`
}

func (c *Client) Operations(ctx context.Context, account, from, to, cursor string) (OperationsPage, error) {
	var r OperationsPage
	if account == "" {
		return r, fmt.Errorf("account required")
	}
	err := c.read(ctx, "OperationsService/GetOperationsByCursor", map[string]any{"accountId": account, "from": from, "to": to, "cursor": cursor, "limit": 100, "withoutCommissions": false, "withoutTrades": false, "withoutOvernights": false}, &r)
	return r, err
}
func (c *Client) Portfolio(ctx context.Context, account string) (json.RawMessage, error) {
	var r json.RawMessage
	err := c.read(ctx, "OperationsService/GetPortfolio", map[string]any{"accountId": account}, &r)
	return r, err
}
func (c *Client) Positions(ctx context.Context, account string) (json.RawMessage, error) {
	var r json.RawMessage
	err := c.read(ctx, "OperationsService/GetPositions", map[string]any{"accountId": account}, &r)
	return r, err
}
func (c *Client) InstrumentByUID(ctx context.Context, uid string) (json.RawMessage, error) {
	var r json.RawMessage
	err := c.read(ctx, "InstrumentsService/GetInstrumentBy", map[string]any{"idType": "INSTRUMENT_ID_TYPE_UID", "id": uid}, &r)
	return r, err
}
