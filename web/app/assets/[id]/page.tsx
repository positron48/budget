"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { useQuery, useMutation } from "@tanstack/react-query";
import { useLocale, useTranslations } from "next-intl";
import { ClientsProvider, useClients } from "@/app/providers";
import { Button, Icon, ConfirmDialog, Protected } from "@/components";
import type {
  AssetAccount,
  AssetHistoryItem,
} from "@/proto/budget/v1/asset_pb";
import {
  ASSET_ICONS,
  AssetKind,
  assetMoney,
  assetDate,
  useAssetTenant,
  useRefreshAssets,
  hasNegativeBalance,
} from "@/lib/assets";
import AssetDialogs, {
  AssetAction,
  assetError,
} from "@/components/assets/AssetDialogs";

type DeleteAction = {
  mode: "snapshot" | "transfer" | "archive" | "delete";
  id: string;
  version: bigint;
};
function AssetAccountDetail() {
  const params = useParams<{ id: string }>();
  const id = params.id;
  const router = useRouter();
  const { asset } = useClients();
  const tenant = useAssetTenant();
  const t = useTranslations("assets");
  const locale = useLocale();
  const refresh = useRefreshAssets();
  const [page, setPage] = useState(1);
  const [action, setAction] = useState<AssetAction | null>(null);
  const [deleting, setDeleting] = useState<DeleteAction | null>(null);
  const [error, setError] = useState("");
  const details = useQuery<{ account: AssetAccount }>({
    queryKey: ["assets", "account", tenant, id],
    enabled: !!tenant,
    queryFn: () => asset.getAccount({ id }),
  });
  const all = useQuery<{ accounts: AssetAccount[] }>({
    queryKey: ["assets", "accounts", tenant, true],
    enabled: !!tenant,
    queryFn: () => asset.listAccounts({ includeArchived: true }),
  });
  const history = useQuery<{
    items: AssetHistoryItem[];
    page: { totalPages: number; totalItems: bigint };
  }>({
    queryKey: ["assets", "history", tenant, id, page],
    enabled: !!tenant,
    queryFn: () =>
      asset.listAccountHistory({ accountId: id, page: { page, pageSize: 25 } }),
  });
  const a = details.data?.account;
  const remove = useMutation({
    mutationFn: async (v: DeleteAction) => {
      if (v.mode === "snapshot")
        await asset.deleteSnapshot({ id: v.id, expectedVersion: v.version });
      else if (v.mode === "transfer")
        await asset.deleteTransfer({ id: v.id, expectedVersion: v.version });
      else if (v.mode === "archive")
        await asset.archiveAccount({ id: v.id, expectedVersion: v.version });
      else
        await asset.deleteEmptyAccount({
          id: v.id,
          expectedVersion: v.version,
        });
    },
    onSuccess: async (_, v) => {
      setDeleting(null);
      setError("");
      await refresh();
      if (v.mode === "delete") router.push("/assets");
    },
    onError: (e) => {
      setDeleting(null);
      setError(assetError(e, t));
    },
  });
  const restore = useMutation({
    mutationFn: () => asset.restoreAccount({ id, expectedVersion: a?.version }),
    onSuccess: async () => {
      setError("");
      await refresh();
    },
    onError: (e) => setError(assetError(e, t)),
  });
  if (details.error)
    return (
      <main className="container mx-auto max-w-7xl p-6">
        <Link href="/assets">← {t("back")}</Link>
        <p role="alert" className="mt-6">
          {t("errors.notFound")}
        </p>
      </main>
    );
  if (!a)
    return (
      <main className="container mx-auto max-w-7xl p-6">
        <div className="h-52 animate-pulse rounded-2xl bg-[hsl(var(--secondary))]" />
      </main>
    );
  const actionsDisabled = a.archived;
  return (
    <main className="asset-page container mx-auto max-w-7xl px-4 py-7 sm:px-6 lg:px-8">
      <Link
        href="/assets"
        className="mb-6 inline-flex items-center gap-1.5 text-sm text-[hsl(var(--muted-foreground))] hover:text-[hsl(var(--primary))]"
      >
        <Icon name="arrow-left" size={16} />
        {t("back")}
      </Link>
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-center gap-4">
          <span className={`asset-kind-icon asset-kind-${a.kind} !h-14 !w-14`}>
            <Icon name={ASSET_ICONS[a.kind as AssetKind]} size={28} />
          </span>
          <div>
            <p className="text-xs text-[hsl(var(--muted-foreground))]">
              {t(`kinds.${a.kind}`)}
              {a.institution ? ` · ${a.institution}` : ""}
            </p>
            <h1 className="mt-1 break-words text-3xl font-semibold tracking-tight">
              {a.name}
            </h1>
            {a.archived && (
              <span className="asset-tag mt-2">{t("archived")}</span>
            )}
          </div>
        </div>
        <Button
          variant="outline"
          icon="edit"
          onClick={() => setAction({ mode: "account", account: a })}
        >
          {t("editAccount")}
        </Button>
      </div>
      {error && (
        <p
          role="alert"
          className="mb-5 rounded-lg bg-[hsl(var(--negative)/0.08)] p-3 text-sm text-[hsl(var(--negative))]"
        >
          {error}
        </p>
      )}
      <section className="asset-total-panel">
        <p className="text-sm text-[hsl(var(--muted-foreground))]">
          {a.kind === "property"
            ? t("estimatedValue")
            : a.kind === "investment"
              ? t("wholeValuation")
              : t("currentBalance")}
        </p>
        <div className="mt-4 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {a.balances.length ? (
            a.balances.map((b) => (
              <div key={b.amount?.currencyCode}>
                <p className="text-3xl font-semibold tracking-tight tabular-nums">
                  {assetMoney(
                    b.amount?.minorUnits,
                    b.amount?.currencyCode,
                    locale,
                  )}
                </p>
                <p className="mt-2 text-xs text-[hsl(var(--muted-foreground))]">
                  {t(
                    a.kind === "property"
                      ? "propertyLastValued"
                      : "lastConfirmed",
                    {
                      amount: assetMoney(
                        b.confirmedAmount?.minorUnits,
                        b.amount?.currencyCode,
                        locale,
                      ),
                      date: assetDate(b.confirmedAt, locale, true),
                    },
                  )}
                </p>
              </div>
            ))
          ) : (
            <p className="text-xl">{t("unknownBalance")}</p>
          )}
        </div>
        {a.kind === "investment" && (
          <p className="mt-4 text-sm text-[hsl(var(--muted-foreground))]">
            {t("investmentHint")}
          </p>
        )}
        {a.kind === "property" && (
          <p className="mt-4 text-sm text-[hsl(var(--muted-foreground))]">
            {t("propertyHint")}
          </p>
        )}
        {hasNegativeBalance(a) && (
          <p className="mt-4 flex items-center gap-2 text-sm text-[hsl(var(--warning))]">
            <Icon name="alert-circle" size={18} />
            {t("negativeWarning")}
          </p>
        )}
        {!actionsDisabled && (
          <div className="mt-6 flex flex-wrap gap-2">
            <Button
              icon="fx"
              onClick={() => setAction({ mode: "snapshot", account: a })}
            >
              {a.kind === "investment" || a.kind === "property"
                ? t("updateValuation")
                : t("reconcile")}
            </Button>
            {a.kind !== "property" && (
              <>
                <Button
                  variant="outline"
                  icon="plus"
                  onClick={() =>
                    setAction({ mode: "transfer", account: a, direction: "in" })
                  }
                >
                  {t("topUp")}
                </Button>
                <Button
                  variant="outline"
                  icon="minus"
                  onClick={() =>
                    setAction({
                      mode: "transfer",
                      account: a,
                      direction: "out",
                    })
                  }
                >
                  {t("withdraw")}
                </Button>
              </>
            )}
            {a.kind !== "investment" && a.kind !== "property" && (
              <>
                <Button
                  variant="ghost"
                  icon="trending-down"
                  onClick={() =>
                    setAction({ mode: "transaction", account: a, txType: 2 })
                  }
                >
                  {t("expense")}
                </Button>
                <Button
                  variant="ghost"
                  icon="trending-up"
                  onClick={() =>
                    setAction({ mode: "transaction", account: a, txType: 1 })
                  }
                >
                  {a.kind === "deposit" ? t("receivedInterest") : t("income")}
                </Button>
                <Button
                  variant="ghost"
                  icon="fx"
                  onClick={() => setAction({ mode: "exchange", account: a })}
                >
                  {t("exchange")}
                </Button>
              </>
            )}
          </div>
        )}
      </section>
      {(a.kind === "deposit" || a.note) && (
        <section className="asset-account-card mt-5">
          <div className="flex flex-wrap gap-7">
            {a.kind === "deposit" && (
              <>
                {a.depositRateDecimal && (
                  <div>
                    <p className="text-xs text-[hsl(var(--muted-foreground))]">
                      {t("rate")}
                    </p>
                    <p className="mt-1 font-semibold">
                      {a.depositRateDecimal}%
                    </p>
                  </div>
                )}
                {a.depositOpenedOn && (
                  <div>
                    <p className="text-xs text-[hsl(var(--muted-foreground))]">
                      {t("openedOn")}
                    </p>
                    <p className="mt-1">{a.depositOpenedOn}</p>
                  </div>
                )}
                {a.depositMaturesOn && (
                  <div>
                    <p className="text-xs text-[hsl(var(--muted-foreground))]">
                      {t("maturesOn")}
                    </p>
                    <p className="mt-1">{a.depositMaturesOn}</p>
                  </div>
                )}
              </>
            )}
          </div>
          {a.note && (
            <p className="mt-3 whitespace-pre-wrap break-words text-sm text-[hsl(var(--muted-foreground))]">
              {a.note}
            </p>
          )}
        </section>
      )}
      <section className="mt-8">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-xl font-semibold">{t("history")}</h2>
          <span className="text-xs text-[hsl(var(--muted-foreground))]">
            {t("eventsCount", {
              count: Number(history.data?.page?.totalItems || 0),
            })}
          </span>
        </div>
        {history.error ? (
          <p role="alert">{t("errors.load")}</p>
        ) : history.isLoading ? (
          <div className="h-32 animate-pulse rounded-xl bg-[hsl(var(--secondary))]" />
        ) : history.data?.items.length ? (
          <div className="asset-history-list">
            {history.data.items.map((item) => {
              const isSnapshot = !!item.snapshot;
              const positive = (item.amount?.minorUnits ?? 0n) > 0n;
              return (
                <div key={item.id} className="asset-history-row">
                  <span
                    className={`asset-history-icon ${isSnapshot ? "" : positive ? "asset-history-positive" : "asset-history-negative"}`}
                  >
                    <Icon
                      name={
                        isSnapshot
                          ? "check"
                          : item.kind === "exchange"
                            ? "fx"
                            : item.kind === "transfer"
                              ? "arrow-right"
                              : positive
                                ? "trending-up"
                                : "trending-down"
                      }
                      size={18}
                    />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">
                      {t(
                        a.kind === "property" && item.kind === "opening"
                          ? "initialPropertyValuation"
                          : `historyKinds.${item.kind}`,
                      )}
                      {item.counterpartyName && (
                        <span className="font-normal text-[hsl(var(--muted-foreground))]">
                          {" "}
                          ·{" "}
                          {item.counterpartyName === "outside"
                            ? t("outside")
                            : item.counterpartyName}
                        </span>
                      )}
                    </p>
                    <p className="mt-0.5 text-xs text-[hsl(var(--muted-foreground))]">
                      {assetDate(item.occurredAt, locale, true)}
                    </p>
                    {item.calculatedAmount && (
                      <p className="mt-1 text-xs text-[hsl(var(--muted-foreground))]">
                        {t("reconciliationDifference", {
                          amount: assetMoney(
                            (item.amount?.minorUnits || 0n) -
                              item.calculatedAmount.minorUnits,
                            item.amount?.currencyCode,
                            locale,
                          ),
                        })}
                      </p>
                    )}
                    {item.note && (
                      <p className="mt-1 break-words text-sm text-[hsl(var(--muted-foreground))]">
                        {item.note}
                      </p>
                    )}
                  </div>
                  <div className="text-right">
                    <p
                      className={`whitespace-nowrap text-sm font-semibold tabular-nums ${isSnapshot ? "" : positive ? "text-[hsl(var(--positive))]" : "text-[hsl(var(--negative))]"}`}
                    >
                      {!isSnapshot && positive ? "+" : ""}
                      {assetMoney(
                        item.amount?.minorUnits,
                        item.amount?.currencyCode,
                        locale,
                      )}
                    </p>
                    {!actionsDisabled && (
                      <div className="mt-1 flex justify-end gap-1">
                        {item.snapshot && (
                          <>
                            <button
                              className="asset-history-action"
                              aria-label={t("editSnapshot")}
                              onClick={() =>
                                setAction({
                                  mode: "snapshot",
                                  account: a,
                                  snapshot: item.snapshot,
                                })
                              }
                            >
                              <Icon name="edit" size={14} />
                            </button>
                            <button
                              className="asset-history-action"
                              aria-label={t("deleteSnapshot")}
                              onClick={() =>
                                setDeleting({
                                  mode: "snapshot",
                                  id: item.snapshot!.id,
                                  version: item.snapshot!.version,
                                })
                              }
                            >
                              <Icon name="trash" size={14} />
                            </button>
                          </>
                        )}
                        {item.transfer && (
                          <>
                            <button
                              className="asset-history-action"
                              aria-label={t("editTransfer")}
                              onClick={() =>
                                setAction({
                                  mode: "transfer",
                                  transfer: item.transfer,
                                })
                              }
                            >
                              <Icon name="edit" size={14} />
                            </button>
                            <button
                              className="asset-history-action"
                              aria-label={t("deleteTransfer")}
                              onClick={() =>
                                setDeleting({
                                  mode: "transfer",
                                  id: item.transfer!.id,
                                  version: item.transfer!.version,
                                })
                              }
                            >
                              <Icon name="trash" size={14} />
                            </button>
                          </>
                        )}
                        {!item.snapshot && !item.transfer && (
                          <Link
                            className="asset-history-action"
                            aria-label={t("openOperation")}
                            href={
                              item.kind === "exchange"
                                ? "/currency-exchanges"
                                : "/transactions"
                            }
                          >
                            <Icon name="external-link" size={14} />
                          </Link>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="asset-empty !py-10">
            <p className="text-sm text-[hsl(var(--muted-foreground))]">
              {t("historyEmpty")}
            </p>
          </div>
        )}
        {(history.data?.page?.totalPages || 0) > 1 && (
          <div className="mt-4 flex justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={page === 1}
              onClick={() => setPage((p) => p - 1)}
            >
              {t("previous")}
            </Button>
            <span className="self-center text-sm">
              {page} / {history.data?.page?.totalPages}
            </span>
            <Button
              size="sm"
              variant="outline"
              disabled={page >= (history.data?.page?.totalPages || 1)}
              onClick={() => setPage((p) => p + 1)}
            >
              {t("next")}
            </Button>
          </div>
        )}
      </section>
      <div className="mt-8 flex flex-wrap items-center justify-between gap-3 border-t border-[hsl(var(--border))] pt-5">
        <p className="text-xs text-[hsl(var(--muted-foreground))]">
          {t(a.kind === "property" ? "propertyArchiveHint" : "archiveHint")}
        </p>
        {a.archived ? (
          <Button
            variant="outline"
            size="sm"
            loading={restore.isPending}
            onClick={() => restore.mutate()}
          >
            {t("restore")}
          </Button>
        ) : (
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              setDeleting({ mode: "archive", id: a.id, version: a.version })
            }
          >
            {t("archiveAccount")}
          </Button>
        )}
        {!a.balances.length && !history.data?.items.length && (
          <Button
            size="sm"
            variant="destructive"
            onClick={() =>
              setDeleting({ mode: "delete", id: a.id, version: a.version })
            }
          >
            {t("deleteAccount")}
          </Button>
        )}
      </div>
      <AssetDialogs
        key={
          action
            ? `${action.mode}-${action.snapshot?.id || action.transfer?.id || "new"}`
            : "closed"
        }
        action={action}
        accounts={all.data?.accounts || [a]}
        onClose={() => setAction(null)}
      />
      <ConfirmDialog
        open={!!deleting}
        title={
          deleting?.mode === "archive" ? t("archiveAccount") : t("deleteTitle")
        }
        message={
          deleting?.mode === "archive"
            ? t(
                a.kind === "property"
                  ? "propertyArchiveConfirm"
                  : "archiveConfirm",
              )
            : t(
                a.kind === "property"
                  ? "deletePropertyValuationConfirm"
                  : "deleteConfirm",
              )
        }
        destructive={deleting?.mode !== "archive"}
        loading={remove.isPending}
        onCancel={() => setDeleting(null)}
        onConfirm={() => deleting && remove.mutate(deleting)}
      />
    </main>
  );
}
export default function AssetAccountPage() {
  return (
    <Protected>
      <ClientsProvider>
        <AssetAccountDetail />
      </ClientsProvider>
    </Protected>
  );
}
