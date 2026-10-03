package grpcadapter

import (
	"context"
	"github.com/google/uuid"
	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type AssetServer struct {
	budgetv1.UnimplementedAssetServiceServer
	svc *asset.Service
}

func NewAssetServer(svc *asset.Service) *AssetServer { return &AssetServer{svc: svc} }
func assetContext(ctx context.Context) (string, string, error) {
	tenant, ok := ctxutil.TenantIDFromContext(ctx)
	user, okU := ctxutil.UserIDFromContext(ctx)
	if !ok || !okU {
		return "", "", status.Error(codes.Unauthenticated, "missing tenant or user")
	}
	return tenant, user, nil
}
func assetID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return invalidArg("invalid object id")
	}
	return nil
}
func assetTimestamp(t *timestamppb.Timestamp) (time.Time, error) {
	if t == nil {
		return time.Time{}, invalidArg("timestamp is required")
	}
	if err := t.CheckValid(); err != nil {
		return time.Time{}, invalidArg("invalid timestamp")
	}
	return t.AsTime(), nil
}
func assetMoney(m *budgetv1.Money) domain.Money {
	return domain.Money{CurrencyCode: m.GetCurrencyCode(), MinorUnits: m.GetMinorUnits()}
}
func protoAssetMoney(m domain.Money) *budgetv1.Money {
	return &budgetv1.Money{CurrencyCode: m.CurrencyCode, MinorUnits: m.MinorUnits}
}
func fromProtoAssetAccount(p *budgetv1.AssetAccount, tenant string) domain.AssetAccount {
	return domain.AssetAccount{ID: p.GetId(), TenantID: tenant, Name: p.GetName(), Kind: p.GetKind(), Institution: p.GetInstitution(), Note: p.GetNote(), FixedCurrencyCode: p.GetFixedCurrencyCode(), DepositRateDecimal: p.GetDepositRateDecimal(), DepositOpenedOn: p.GetDepositOpenedOn(), DepositMaturesOn: p.GetDepositMaturesOn(), Version: p.GetVersion()}
}
func toProtoAssetAccount(a domain.AssetAccount) *budgetv1.AssetAccount {
	out := &budgetv1.AssetAccount{Id: a.ID, TenantId: a.TenantID, Name: a.Name, Kind: a.Kind, Institution: a.Institution, Note: a.Note, FixedCurrencyCode: a.FixedCurrencyCode, DepositRateDecimal: a.DepositRateDecimal, DepositOpenedOn: a.DepositOpenedOn, DepositMaturesOn: a.DepositMaturesOn, Archived: a.Archived, Version: a.Version, CreatedAt: timestamppb.New(a.CreatedAt)}
	for _, b := range a.Balances {
		out.Balances = append(out.Balances, &budgetv1.AssetBalance{Amount: protoAssetMoney(b.Amount), ConfirmedAmount: protoAssetMoney(b.ConfirmedAmount), ConfirmedAt: timestamppb.New(b.ConfirmedAt)})
	}
	return out
}
func fromProtoAssetSnapshot(p *budgetv1.AssetSnapshot, tenant, user string) (domain.AssetSnapshot, error) {
	at, err := assetTimestamp(p.GetAsOf())
	if err != nil {
		return domain.AssetSnapshot{}, err
	}
	return domain.AssetSnapshot{ID: p.GetId(), TenantID: tenant, AccountID: p.GetAccountId(), UserID: user, Amount: assetMoney(p.GetAmount()), AsOf: at, Kind: p.GetKind(), Note: p.GetNote(), Version: p.GetVersion()}, nil
}
func toProtoAssetSnapshot(s domain.AssetSnapshot) *budgetv1.AssetSnapshot {
	return &budgetv1.AssetSnapshot{Id: s.ID, AccountId: s.AccountID, Amount: protoAssetMoney(s.Amount), AsOf: timestamppb.New(s.AsOf), Kind: s.Kind, Note: s.Note, Version: s.Version, CreatedAt: timestamppb.New(s.CreatedAt), UserId: s.UserID}
}
func fromProtoAssetTransfer(p *budgetv1.AssetTransfer, tenant, user string) (domain.AssetTransfer, error) {
	at, err := assetTimestamp(p.GetOccurredAt())
	if err != nil {
		return domain.AssetTransfer{}, err
	}
	for _, id := range []string{p.GetFromAccountId(), p.GetToAccountId()} {
		if id != "" {
			if err = assetID(id); err != nil {
				return domain.AssetTransfer{}, err
			}
		}
	}
	return domain.AssetTransfer{ID: p.GetId(), TenantID: tenant, UserID: user, FromAccountID: p.GetFromAccountId(), ToAccountID: p.GetToAccountId(), FromAmount: assetMoney(p.GetFromAmount()), ToAmount: assetMoney(p.GetToAmount()), OccurredAt: at, Note: p.GetNote(), Version: p.GetVersion()}, nil
}
func toProtoAssetTransfer(t domain.AssetTransfer) *budgetv1.AssetTransfer {
	return &budgetv1.AssetTransfer{Id: t.ID, FromAccountId: t.FromAccountID, ToAccountId: t.ToAccountID, FromAmount: protoAssetMoney(t.FromAmount), ToAmount: protoAssetMoney(t.ToAmount), OccurredAt: timestamppb.New(t.OccurredAt), Note: t.Note, Version: t.Version, CreatedAt: timestamppb.New(t.CreatedAt), UserId: t.UserID}
}

func (s *AssetServer) CreateAccount(ctx context.Context, req *budgetv1.CreateAccountRequest) (*budgetv1.CreateAccountResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	a := fromProtoAssetAccount(req.GetAccount(), tenant)
	a.ID = ""
	a.Version = 0
	openings := []domain.AssetSnapshot{}
	for _, p := range req.GetOpeningBalances() {
		v, e := fromProtoAssetSnapshot(p, tenant, user)
		if e != nil {
			return nil, e
		}
		v.ID = ""
		v.Version = 0
		openings = append(openings, v)
	}
	result, err := s.svc.CreateAccount(ctx, a, openings, req.GetRequestKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.CreateAccountResponse{Account: toProtoAssetAccount(result)}, nil
}
func (s *AssetServer) UpdateAccount(ctx context.Context, req *budgetv1.UpdateAccountRequest) (*budgetv1.UpdateAccountResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	a := fromProtoAssetAccount(req.GetAccount(), tenant)
	if err = assetID(a.ID); err != nil {
		return nil, err
	}
	result, err := s.svc.UpdateAccount(ctx, a)
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.UpdateAccountResponse{Account: toProtoAssetAccount(result)}, nil
}
func (s *AssetServer) GetAccount(ctx context.Context, req *budgetv1.GetAccountRequest) (*budgetv1.GetAccountResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetId()); err != nil {
		return nil, err
	}
	result, err := s.svc.GetAccount(ctx, tenant, req.GetId())
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.GetAccountResponse{Account: toProtoAssetAccount(result)}, nil
}
func (s *AssetServer) ListAccounts(ctx context.Context, req *budgetv1.ListAccountsRequest) (*budgetv1.ListAccountsResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	results, err := s.svc.ListAccounts(ctx, tenant, req.GetIncludeArchived())
	if err != nil {
		return nil, mapError(err)
	}
	out := &budgetv1.ListAccountsResponse{}
	for _, a := range results {
		out.Accounts = append(out.Accounts, toProtoAssetAccount(a))
	}
	return out, nil
}
func (s *AssetServer) ArchiveAccount(ctx context.Context, req *budgetv1.ArchiveAccountRequest) (*budgetv1.ArchiveAccountResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetId()); err != nil {
		return nil, err
	}
	a, err := s.svc.SetArchived(ctx, tenant, req.GetId(), req.GetExpectedVersion(), true)
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.ArchiveAccountResponse{Account: toProtoAssetAccount(a)}, nil
}
func (s *AssetServer) RestoreAccount(ctx context.Context, req *budgetv1.RestoreAccountRequest) (*budgetv1.RestoreAccountResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetId()); err != nil {
		return nil, err
	}
	a, err := s.svc.SetArchived(ctx, tenant, req.GetId(), req.GetExpectedVersion(), false)
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.RestoreAccountResponse{Account: toProtoAssetAccount(a)}, nil
}
func (s *AssetServer) DeleteEmptyAccount(ctx context.Context, req *budgetv1.DeleteEmptyAccountRequest) (*budgetv1.DeleteEmptyAccountResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetId()); err != nil {
		return nil, err
	}
	if err = s.svc.DeleteEmptyAccount(ctx, tenant, req.GetId(), req.GetExpectedVersion()); err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.DeleteEmptyAccountResponse{}, nil
}
func (s *AssetServer) CreateSnapshot(ctx context.Context, req *budgetv1.CreateSnapshotRequest) (*budgetv1.CreateSnapshotResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := fromProtoAssetSnapshot(req.GetSnapshot(), tenant, user)
	if err != nil {
		return nil, err
	}
	if err = assetID(v.AccountID); err != nil {
		return nil, err
	}
	v.ID = ""
	v.Version = 0
	result, err := s.svc.CreateSnapshot(ctx, v, req.GetRequestKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.CreateSnapshotResponse{Snapshot: toProtoAssetSnapshot(result)}, nil
}
func (s *AssetServer) UpdateSnapshot(ctx context.Context, req *budgetv1.UpdateSnapshotRequest) (*budgetv1.UpdateSnapshotResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := fromProtoAssetSnapshot(req.GetSnapshot(), tenant, user)
	if err != nil {
		return nil, err
	}
	if err = assetID(v.ID); err != nil {
		return nil, err
	}
	result, err := s.svc.UpdateSnapshot(ctx, v)
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.UpdateSnapshotResponse{Snapshot: toProtoAssetSnapshot(result)}, nil
}
func (s *AssetServer) DeleteSnapshot(ctx context.Context, req *budgetv1.DeleteSnapshotRequest) (*budgetv1.DeleteSnapshotResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetId()); err != nil {
		return nil, err
	}
	if err = s.svc.DeleteSnapshot(ctx, tenant, req.GetId(), req.GetExpectedVersion()); err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.DeleteSnapshotResponse{}, nil
}
func (s *AssetServer) CreateTransfer(ctx context.Context, req *budgetv1.CreateTransferRequest) (*budgetv1.CreateTransferResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := fromProtoAssetTransfer(req.GetTransfer(), tenant, user)
	if err != nil {
		return nil, err
	}
	v.ID = ""
	v.Version = 0
	v.RequestKey = req.GetRequestKey()
	result, err := s.svc.CreateTransfer(ctx, v)
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.CreateTransferResponse{Transfer: toProtoAssetTransfer(result)}, nil
}
func (s *AssetServer) UpdateTransfer(ctx context.Context, req *budgetv1.UpdateTransferRequest) (*budgetv1.UpdateTransferResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := fromProtoAssetTransfer(req.GetTransfer(), tenant, user)
	if err != nil {
		return nil, err
	}
	if err = assetID(v.ID); err != nil {
		return nil, err
	}
	result, err := s.svc.UpdateTransfer(ctx, v)
	if err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.UpdateTransferResponse{Transfer: toProtoAssetTransfer(result)}, nil
}
func (s *AssetServer) DeleteTransfer(ctx context.Context, req *budgetv1.DeleteTransferRequest) (*budgetv1.DeleteTransferResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetId()); err != nil {
		return nil, err
	}
	if err = s.svc.DeleteTransfer(ctx, tenant, req.GetId(), req.GetExpectedVersion()); err != nil {
		return nil, mapError(err)
	}
	return &budgetv1.DeleteTransferResponse{}, nil
}
func (s *AssetServer) GetOverview(ctx context.Context, req *budgetv1.GetOverviewRequest) (*budgetv1.GetOverviewResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Time{}
	if req.GetAsOf() != nil {
		at, err = assetTimestamp(req.GetAsOf())
		if err != nil {
			return nil, err
		}
	}
	v, err := s.svc.GetOverview(ctx, tenant, req.GetTargetCurrencyCode(), at)
	if err != nil {
		return nil, mapError(err)
	}
	out := &budgetv1.GetOverviewResponse{Total: protoAssetMoney(v.Total), Incomplete: v.Incomplete, MissingCurrencies: v.MissingCurrencies, AsOf: timestamppb.New(v.AsOf)}
	for _, a := range v.Accounts {
		out.Accounts = append(out.Accounts, toProtoAssetAccount(a))
	}
	for _, r := range v.Rates {
		out.Rates = append(out.Rates, &budgetv1.FxRate{FromCurrencyCode: r.From, ToCurrencyCode: r.To, RateDecimal: r.RateDecimal, AsOf: timestamppb.New(r.AsOf), Provider: r.Provider})
	}
	return out, nil
}
func (s *AssetServer) ListAccountHistory(ctx context.Context, req *budgetv1.ListAccountHistoryRequest) (*budgetv1.ListAccountHistoryResponse, error) {
	tenant, _, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err = assetID(req.GetAccountId()); err != nil {
		return nil, err
	}
	page, size := int(req.GetPage().GetPage()), int(req.GetPage().GetPageSize())
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 25
	}
	items, total, err := s.svc.ListAccountHistory(ctx, tenant, req.GetAccountId(), page, size)
	if err != nil {
		return nil, mapError(err)
	}
	out := &budgetv1.ListAccountHistoryResponse{Page: &budgetv1.PageResponse{Page: int32(page), PageSize: int32(size), TotalItems: total, TotalPages: int32((total + int64(size) - 1) / int64(size))}}
	for _, h := range items {
		v := &budgetv1.AssetHistoryItem{Id: h.ID, Kind: h.Kind, Amount: protoAssetMoney(h.Amount), OccurredAt: timestamppb.New(h.OccurredAt), Note: h.Note, SourceId: h.SourceID, CounterpartyName: h.CounterpartyName, UserId: h.UserID}
		if h.CalculatedAmount != nil {
			v.CalculatedAmount = protoAssetMoney(*h.CalculatedAmount)
		}
		if h.Snapshot != nil {
			v.Snapshot = toProtoAssetSnapshot(*h.Snapshot)
		}
		if h.Transfer != nil {
			v.Transfer = toProtoAssetTransfer(*h.Transfer)
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}
