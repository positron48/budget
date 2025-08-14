"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useClients } from "@/app/providers";
import { Button, Icon } from "@/components";
import { useLocale, useTranslations } from "next-intl";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CategoryKind, TransactionType } from "@/proto/budget/v1/common_pb";
import CategoryMapping from "./CategoryMapping";

type ParsedCsv = { headers: string[]; rows: string[][] };

interface CsvMapping {
  dateColumn?: string;
  amountColumn?: string;
  currencyCodeColumn?: string;
  typeColumn?: string;
  categoryColumn?: string;
  commentColumn?: string;
}

interface Props {
  onClose: () => void;
  onCompleted: (inserted: number) => void;
}

function detectDelimiter(sample: string): string {
  const candidates = [",", ";", "\t", "|"]; 
  const scores = candidates.map((d) => ({ d, c: (sample.match(new RegExp(`\\${d}`, "g")) || []).length }));
  scores.sort((a, b) => b.c - a.c);
  return scores[0]?.d || ",";
}

function parseCsv(text: string, delimiter: string, quote: string = '"'): ParsedCsv {
  const rows: string[][] = [];
  let current: string[] = [];
  let cell = "";
  let inQuotes = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    const next = text[i + 1];
    if (inQuotes) {
      if (ch === quote) {
        if (next === quote) {
          cell += quote;
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        cell += ch;
      }
    } else {
      if (ch === quote) {
        inQuotes = true;
      } else if (ch === delimiter) {
        current.push(cell);
        cell = "";
      } else if (ch === "\n") {
        current.push(cell);
        rows.push(current);
        current = [];
        cell = "";
      } else if (ch === "\r") {
        // skip
      } else {
        cell += ch;
      }
    }
  }
  if (cell.length > 0 || current.length > 0) {
    current.push(cell);
    rows.push(current);
  }
  const headers = rows[0] || [];
  const dataRows = rows.slice(1);
  return { headers, rows: dataRows };
}

function normalizeHeader(h: string): string {
  return (h || "").trim().toLowerCase();
}

function guessMapping(headers: string[]): CsvMapping {
  const mapBy = (names: string[]): string | undefined => {
    const lower = headers.map((h) => normalizeHeader(h));
    for (const n of names) {
      const idx = lower.indexOf(n);
      if (idx >= 0) return headers[idx];
    }
    return undefined;
  };
  return {
    dateColumn: mapBy(["date", "дата", "occurred", "occurred_at", "posted", "дата операции"]),
    amountColumn: mapBy(["amount", "сумма", "value", "debit", "credit"]),
    currencyCodeColumn: mapBy(["currency", "валюта", "currency_code"]),
    typeColumn: mapBy(["type", "тип", "direction", "income_expense"]),
    categoryColumn: mapBy(["category", "категория", "cat", "category_name"]),
    commentColumn: mapBy(["comment", "комментарий", "description", "memo", "note"]),
  };
}

function parseAmountToMinorUnits(raw: string): number | null {
  if (!raw) return null;
  // Extract numeric part at the start, allowing spaces, thousand seps, comma/dot decimals
  const trimmed = String(raw).trim();
  const match = trimmed.match(/^([+\-]?\s*[0-9\s]+(?:[\.,][0-9]{1,2})?)/);
  if (!match) return null;
  const numericPart = match[1];
  const noSpaces = numericPart.replace(/\s+/g, "");
  const normalized = noSpaces.replace(/,/g, ".");
  const val = Number(normalized);
  if (Number.isNaN(val)) return null;
  return Math.round(val * 100);
}

function parseDateToSeconds(raw: string): number | null {
  if (!raw) return null;
  const s = String(raw).trim();

  // ISO-like: YYYY-MM-DD [HH:mm[:ss]] (allow separators -, /, . and T/space)
  let m = s.match(/^(\d{4})[.\/-](\d{1,2})[.\/-](\d{1,2})(?:[T\s]+(\d{1,2}):(\d{2})(?::(\d{2}))?)?$/);
  if (m) {
    const [, yyyy, mm, dd, hh = "00", mi = "00", ss = "00"] = m;
    const d = new Date(Number(yyyy), Number(mm) - 1, Number(dd), Number(hh), Number(mi), Number(ss));
    return Math.floor(d.getTime() / 1000);
  }

  // D.M.Y or D-M-Y or D/M/Y or D,M,Y (treat strictly as day-month-year)
  m = s.match(/^(\d{1,2})[.,\/-](\d{1,2})[.,\/-](\d{2,4})(?:\s+(\d{1,2}):(\d{2})(?::(\d{2}))?)?$/);
  if (m) {
    const [, dd, mm, yRaw, hh = "00", mi = "00", ss = "00"] = m;
    const y = yRaw.length === 2 ? String(2000 + Number(yRaw)) : yRaw;
    const d = new Date(Number(y), Number(mm) - 1, Number(dd), Number(hh), Number(mi), Number(ss));
    return Math.floor(d.getTime() / 1000);
  }

  // As a last resort, accept RFC3339/ISO complete strings with Date.parse (contains 'T')
  if (s.includes("T")) {
    const t = Date.parse(s);
    if (!Number.isNaN(t)) return Math.floor(t / 1000);
  }
  return null;
}

function toTransactionType(raw: string | null, amountMinorUnits: number | null): TransactionType | null {
  if (raw) {
    const v = raw.trim().toLowerCase();
    if (["expense", "расход", "debit", "-"].includes(v)) return TransactionType.EXPENSE;
    if (["income", "доход", "credit", "+"].includes(v)) return TransactionType.INCOME;
  }
  if (amountMinorUnits != null) {
    if (amountMinorUnits < 0) return TransactionType.EXPENSE;
    if (amountMinorUnits > 0) return TransactionType.INCOME;
  }
  return null;
}

export default function ImportWizard({ onClose, onCompleted }: Props) {
  const { transaction, category, tenant } = useClients();
  const t = useTranslations("transactions");
  const tc = useTranslations("common");
  const locale = useLocale();
  const qc = useQueryClient();

  const [step, setStep] = useState<number>(0);
  const [_file, setFile] = useState<File | null>(null);
  const [delimiter, setDelimiter] = useState<string>(",");
  const [quote, setQuote] = useState<string>('"');
  const [text, setText] = useState<string>("");
  const [parsed, setParsed] = useState<ParsedCsv | null>(null);
  const [mapping, setMapping] = useState<CsvMapping>({});
  const [autoCreateMissingCategories, setAutoCreateMissingCategories] = useState<boolean>(false);
  const [commitLoading, setCommitLoading] = useState<boolean>(false);
  const [statusMsg, setStatusMsg] = useState<string>("");
  const [defaultCurrency, setDefaultCurrency] = useState<string>("RUB");
  const [selectedFileName, setSelectedFileName] = useState<string>("");
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const dragCounterRef = useRef(0);
  const [manualCategoryMap, setManualCategoryMap] = useState<Record<string, string>>({});

  const fileInputRef = useRef<HTMLInputElement>(null);

  // Load default currency from active tenant
  const { data: tenantsData } = useQuery({
    queryKey: ["tenants-for-default-currency"],
    queryFn: async () => (await tenant.listMyTenants({} as any)) as any,
    staleTime: 5 * 60 * 1000,
  });
  useEffect(() => {
    const memberships = tenantsData?.memberships || [];
    let currentTenantId: string | undefined;
    if (typeof window !== "undefined") {
      currentTenantId = window.localStorage.getItem("budget/tenant") || undefined;
    }
    const active = (memberships as any[]).find((m) => m?.tenant?.id === currentTenantId) || memberships[0];
    const dc = active?.tenant?.defaultCurrencyCode || active?.tenant?.default_currency_code || "RUB";
    if (dc) setDefaultCurrency(dc);
  }, [tenantsData]);

  const { data: allCategories } = useQuery({
    queryKey: ["all-categories"],
    queryFn: async () => {
      const [inc, exp] = await Promise.all([
        category.listCategories({ kind: CategoryKind.INCOME, includeInactive: false } as any),
        category.listCategories({ kind: CategoryKind.EXPENSE, includeInactive: false } as any),
      ]);
      return [...(inc.categories || []), ...(exp.categories || [])];
    },
    staleTime: 5 * 60 * 1000,
  });

  const categoryIndex = useMemo(() => {
    const map = new Map<string, any>();
    for (const c of allCategories || []) {
      map.set((c.code || "").toLowerCase(), c);
      for (const tr of c.translations || []) {
        map.set((tr.name || "").toLowerCase(), c);
      }
    }
    return map;
  }, [allCategories]);

  const distinctCategoryNames = useMemo(() => {
    if (!parsed || !mapping.categoryColumn) return [] as string[];
    const idx = parsed.headers.indexOf(mapping.categoryColumn);
    if (idx < 0) return [];
    const set = new Set<string>();
    for (const r of parsed.rows) {
      const v = (r[idx] || "").trim();
      if (v) set.add(v);
    }
    return Array.from(set.values()).sort();
  }, [parsed, mapping]);

  const matchedCategoryMap = useMemo(() => {
    const m = new Map<string, any>();
    for (const name of distinctCategoryNames) {
      const found = categoryIndex.get(name.toLowerCase());
      if (found) m.set(name, found);
    }
    return m;
  }, [distinctCategoryNames, categoryIndex]);

  const missingCategoryNames = useMemo(() => {
    return distinctCategoryNames.filter((n) => !matchedCategoryMap.has(n));
  }, [distinctCategoryNames, matchedCategoryMap]);

  // Suggest category kind per missing category based on file rows (type or amount sign)
  const suggestedCategoryKinds = useMemo(() => {
    const result: Record<string, CategoryKind> = {};
    if (!parsed || !mapping.categoryColumn) return result;
    const counts: Record<string, { inc: number; exp: number }> = {};
    const idx = {
      date: mapping.dateColumn ? parsed.headers.indexOf(mapping.dateColumn) : -1,
      amount: mapping.amountColumn ? parsed.headers.indexOf(mapping.amountColumn) : -1,
      type: mapping.typeColumn ? parsed.headers.indexOf(mapping.typeColumn) : -1,
      category: mapping.categoryColumn ? parsed.headers.indexOf(mapping.categoryColumn) : -1,
    };
    for (const r of parsed.rows) {
      const cat = idx.category >= 0 ? (r[idx.category] || "").trim() : "";
      if (!cat || !missingCategoryNames.includes(cat)) continue;
      const amt = idx.amount >= 0 ? parseAmountToMinorUnits(r[idx.amount]) : null;
      const tp = idx.type >= 0 ? toTransactionType(r[idx.type], amt) : toTransactionType(null, amt);
      if (!counts[cat]) counts[cat] = { inc: 0, exp: 0 };
      if (tp === TransactionType.INCOME) counts[cat].inc++;
      else if (tp === TransactionType.EXPENSE) counts[cat].exp++;
    }
    for (const name of missingCategoryNames) {
      const c = counts[name];
      if (!c) { result[name] = CategoryKind.EXPENSE; continue; }
      result[name] = c.inc >= c.exp ? CategoryKind.INCOME : CategoryKind.EXPENSE;
    }
    return result;
  }, [parsed, mapping, missingCategoryNames]);

  const handleFileSelected = async (f: File) => {
    setFile(f);
    setSelectedFileName(f.name || "");
    const txt = await f.text();
    setText(txt);
    const firstLine = txt.split(/\r?\n/)[0] || "";
    const d = detectDelimiter(firstLine);
    setDelimiter(d);
    const res = parseCsv(txt, d, quote);
    setParsed(res);
    setMapping(guessMapping(res.headers));
    setStep(1);
  };

  const handleDragEnter = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current += 1;
    if (!isDragging) setIsDragging(true);
  };
  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current -= 1;
    if (dragCounterRef.current <= 0) setIsDragging(false);
  };
  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    if (!isDragging) setIsDragging(true);
  };
  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current = 0;
    setIsDragging(false);
    const files = e.dataTransfer?.files;
    if (!files || files.length === 0) return;
    const f = files[0];
    if (!f) return;
    const isCsv = f.type === "text/csv" || f.name.toLowerCase().endsWith(".csv");
    if (!isCsv) return;
    handleFileSelected(f);
  };
  useEffect(() => {
    const reset = () => { dragCounterRef.current = 0; setIsDragging(false); };
    if (typeof window !== "undefined") {
      window.addEventListener("drop", reset);
      window.addEventListener("dragend", reset);
    }
    return () => {
      if (typeof window !== "undefined") {
        window.removeEventListener("drop", reset);
        window.removeEventListener("dragend", reset);
      }
    };
  }, []);

  const preview = useMemo(() => {
    if (!parsed) return { total: 0, valid: 0, invalid: 0, sample: [] as any[] };
    const idx = {
      date: mapping.dateColumn ? parsed.headers.indexOf(mapping.dateColumn) : -1,
      amount: mapping.amountColumn ? parsed.headers.indexOf(mapping.amountColumn) : -1,
      currency: mapping.currencyCodeColumn ? parsed.headers.indexOf(mapping.currencyCodeColumn) : -1,
      type: mapping.typeColumn ? parsed.headers.indexOf(mapping.typeColumn) : -1,
      category: mapping.categoryColumn ? parsed.headers.indexOf(mapping.categoryColumn) : -1,
      comment: mapping.commentColumn ? parsed.headers.indexOf(mapping.commentColumn) : -1,
    };
    let valid = 0;
    let invalid = 0;
    const sample: any[] = [];
    for (let i = 0; i < parsed.rows.length; i++) {
      const r = parsed.rows[i];
      const rawAmount = idx.amount >= 0 ? r[idx.amount] : "";
      const amountMinorUnitsRaw = parseAmountToMinorUnits(rawAmount);
      const rawType = idx.type >= 0 ? r[idx.type] : null;
      const type = toTransactionType(rawType, amountMinorUnitsRaw);
      const rawDate = idx.date >= 0 ? r[idx.date] : "";
      const seconds = parseDateToSeconds(rawDate);
      const ok = seconds != null && amountMinorUnitsRaw != null && type != null;
      if (ok) valid++; else invalid++;
      if (sample.length < 10) {
        sample.push({
          date: rawDate,
          amount: rawAmount,
          currency: idx.currency >= 0 ? (r[idx.currency] || defaultCurrency) : defaultCurrency,
          type: rawType ?? "",
          category: idx.category >= 0 ? r[idx.category] : "",
          comment: idx.comment >= 0 ? r[idx.comment] : "",
          ok,
        });
      }
    }
    return { total: parsed.rows.length, valid, invalid, sample };
  }, [parsed, mapping, defaultCurrency]);

  const handleCommit = async () => {
    if (!parsed) return;
    setCommitLoading(true);
    setStatusMsg(t("importStatusPreparing") as string);
    try {
      const normalizeName = (s: string) => (s || "").trim().toLowerCase();
      // Prepare category mapping: name -> id (existing or to create)
      const nameToCategoryId = new Map<string, string>();
      for (const name of distinctCategoryNames) {
        const match = matchedCategoryMap.get(name);
        if (match) {
          nameToCategoryId.set(name, match.id as string);
        }
      }
      // Also normalized map to be robust to case/space differences
      const normalizedMatched = new Map<string, string>();
      for (const [orig, cat] of Array.from(matchedCategoryMap.entries())) {
        normalizedMatched.set(normalizeName(orig), (cat as any).id as string);
      }
      // Merge manual mapping collected during step 2 (lowercased keys)
      const normalizedManual = new Map<string, string>();
      if (manualCategoryMap) {
        for (const [k, v] of Object.entries(manualCategoryMap)) {
          if (v) normalizedManual.set(normalizeName(k), v);
        }
      }

      // Create missing if opted in
      if (autoCreateMissingCategories && missingCategoryNames.length > 0) {
        for (const name of missingCategoryNames) {
          // Determine kind by heuristic: if the first row with this category is expense/income
          let inferredKind: CategoryKind = CategoryKind.EXPENSE;
          if (mapping.categoryColumn) {
            const catIdx = parsed.headers.indexOf(mapping.categoryColumn);
            const typeIdx = mapping.typeColumn ? parsed.headers.indexOf(mapping.typeColumn) : -1;
            const amountIdx = mapping.amountColumn ? parsed.headers.indexOf(mapping.amountColumn) : -1;
            for (const r of parsed.rows) {
              if ((r[catIdx] || "").trim() === name) {
                const a = amountIdx >= 0 ? parseAmountToMinorUnits(r[amountIdx]) : null;
                const tp = typeIdx >= 0 ? toTransactionType(r[typeIdx], a) : toTransactionType(null, a);
                if (tp === TransactionType.INCOME) { inferredKind = CategoryKind.INCOME; break; }
              }
            }
          }
          setStatusMsg(`${t("importStatusCreatingCategory") as string} ${name}`);
          const resp = await category.createCategory({
            kind: inferredKind,
            code: name,
            isActive: true,
            translations: [
              { locale: locale || "ru", name },
            ],
          } as any);
          nameToCategoryId.set(name, resp.category?.id as string);
        }
      }

      // Create transactions
      let inserted = 0;
      const idx = {
        date: mapping.dateColumn ? parsed.headers.indexOf(mapping.dateColumn) : -1,
        amount: mapping.amountColumn ? parsed.headers.indexOf(mapping.amountColumn) : -1,
        currency: mapping.currencyCodeColumn ? parsed.headers.indexOf(mapping.currencyCodeColumn) : -1,
        type: mapping.typeColumn ? parsed.headers.indexOf(mapping.typeColumn) : -1,
        category: mapping.categoryColumn ? parsed.headers.indexOf(mapping.categoryColumn) : -1,
        comment: mapping.commentColumn ? parsed.headers.indexOf(mapping.commentColumn) : -1,
      };
      for (let i = 0; i < parsed.rows.length; i++) {
        const r = parsed.rows[i];
        const amountMinorUnitsRaw = parseAmountToMinorUnits(idx.amount >= 0 ? r[idx.amount] : "");
        const seconds = parseDateToSeconds(idx.date >= 0 ? r[idx.date] : "");
        const tp = toTransactionType(idx.type >= 0 ? r[idx.type] : null, amountMinorUnitsRaw);
        if (amountMinorUnitsRaw == null || seconds == null || tp == null) continue;
        const currencyCode = idx.currency >= 0 ? (r[idx.currency] || defaultCurrency) : defaultCurrency;
        const catNameRaw = idx.category >= 0 ? (r[idx.category] || "") : "";
        const catName = catNameRaw.trim();
        const norm = normalizeName(catName);
        const manualMapped = norm ? normalizedManual.get(norm) : undefined;
        const categoryId = manualMapped || (norm ? normalizedMatched.get(norm) : undefined);
        const comment = idx.comment >= 0 ? (r[idx.comment] || "") : "";
        setStatusMsg(`${t("importStatusInserting") as string} ${i + 1} / ${parsed.rows.length}`);
        await transaction.createTransaction({
          type: tp,
          amount: { currencyCode, minorUnits: Math.abs(amountMinorUnitsRaw) },
          occurredAt: { seconds },
          categoryId,
          comment,
        } as any);
        inserted++;
      }
      qc.invalidateQueries({ queryKey: ["transactions"] });
      qc.invalidateQueries({ queryKey: ["categories"] });
      onCompleted(inserted);
      onClose();
    } finally {
      setCommitLoading(false);
      setStatusMsg("");
    }
  };

  return (
    <div
      className={`relative space-y-4`}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {isDragging && (
        <div className="absolute inset-0 z-10 flex items-center justify-center rounded-md bg-slate-100/85 dark:bg-slate-800/70 border-2 border-dashed border-slate-400">
          <div className="text-center">
            <div className="text-lg font-semibold text-slate-800 dark:text-slate-100">{t("dragOverlayTitle") as string}</div>
            <div className="mt-1 text-sm text-slate-700 dark:text-slate-200">{t("dragOverlaySubtitle") as string}</div>
          </div>
        </div>
      )}
      {/* Step header */}
      <div className="flex items-center gap-2 text-sm">
        <span className={`px-2 py-0.5 rounded ${step >= 0 ? "bg-blue-600 text-white" : "bg-slate-200"}`}>1</span>
        <span className={step === 0 ? "font-semibold" : "text-slate-600"}>{t("importStepFile")}</span>
        <Icon name="chevron-right" size={14} className="text-slate-400" />
        <span className={`px-2 py-0.5 rounded ${step >= 1 ? "bg-blue-600 text-white" : "bg-slate-200"}`}>2</span>
        <span className={step === 1 ? "font-semibold" : "text-slate-600"}>{t("importStepMapping")}</span>
        {(!autoCreateMissingCategories && missingCategoryNames.length > 0) ? (
          <>
            <Icon name="chevron-right" size={14} className="text-slate-400" />
            <span className={`px-2 py-0.5 rounded ${step >= 2 ? "bg-blue-600 text-white" : "bg-slate-200"}`}>3</span>
            <span className={step === 2 ? "font-semibold" : "text-slate-600"}>{t("importStepCategoryMap")}</span>
            <Icon name="chevron-right" size={14} className="text-slate-400" />
            <span className={`px-2 py-0.5 rounded ${step >= 3 ? "bg-blue-600 text-white" : "bg-slate-200"}`}>4</span>
            <span className={step === 3 ? "font-semibold" : "text-slate-600"}>{t("importStepPreview")}</span>
          </>
        ) : (
          <>
            <Icon name="chevron-right" size={14} className="text-slate-400" />
            <span className={`px-2 py-0.5 rounded ${step >= 2 ? "bg-blue-600 text-white" : "bg-slate-200"}`}>3</span>
            <span className={step === 2 ? "font-semibold" : "text-slate-600"}>{t("importStepPreview")}</span>
          </>
        )}
      </div>

      {/* Step 0: instructions + file */}
      {step === 0 && (
        <div className="space-y-4">
          <div className="rounded-md border border-slate-200 dark:border-slate-700 p-3 bg-slate-50/50 dark:bg-slate-800/40">
            <div className="text-sm font-medium text-slate-900 dark:text-slate-100">{t("importInstructionsTitle")}</div>
            <ul className="mt-2 list-disc pl-5 text-sm text-slate-700 dark:text-slate-300 space-y-1">
              {(t.raw("importInstructionsList") as string[]).map((line, idx) => (
                <li key={idx}>{line}</li>
              ))}
            </ul>
            <div className="mt-2 text-xs text-slate-500">{t("importSampleHeader")}</div>
            <pre className="mt-1 text-xs p-2 bg-white dark:bg-slate-900 rounded border border-slate-200 dark:border-slate-700 overflow-x-auto">{locale === "ru" ? `date,amount,currency,type,category,comment\n2024-01-02,1200.50,RUB,expense,Еда,Обед\n2024-01-03,50000,RUB,income,Зарплата,Аванс\n` : `date,amount,currency,type,category,comment\n2024-01-02,1200.50,USD,expense,Food,Lunch\n2024-01-03,50000,USD,income,Salary,Advance\n`}</pre>
          </div>
          <div className="flex items-center justify-center gap-3">
            <input
              ref={fileInputRef}
              type="file"
              accept=".csv,text/csv"
              className="hidden"
              aria-hidden="true"
              onChange={(e) => e.target.files && e.target.files[0] && handleFileSelected(e.target.files[0])}
            />
            <Button
              variant="secondary"
              icon="upload"
              onClick={() => fileInputRef.current?.click()}
              aria-label={t("chooseFile") as string}
            >
              {t("chooseFile") as string}
            </Button>
            {selectedFileName && (
              <span className="text-sm text-slate-600 dark:text-slate-300 truncate max-w-[50%]" title={selectedFileName}>
                {selectedFileName}
              </span>
            )}
          </div>

          {/* Small hint (global drag handlers are on container) */}
          <div className="mt-3 w-full text-center text-xs text-slate-500 select-none">
            {t("dragDropHint") as string}
          </div>
        </div>
      )}

      {/* Step 1: mapping */}
      {step === 1 && parsed && (
        <div className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-slate-500">{t("delimiter")}</label>
              <select className="w-full border rounded px-2 py-1 bg-white dark:bg-slate-700" value={delimiter} onChange={(e) => {
                const d = e.target.value; setDelimiter(d); if (text) { const res = parseCsv(text, d, quote); setParsed(res); setMapping(guessMapping(res.headers)); }
              }}>
                <option value=",">,</option>
                <option value=";">;</option>
                <option value="\t">TAB</option>
                <option value="|">|</option>
              </select>
            </div>
            <div>
              <label className="text-xs text-slate-500">{t("quote")}</label>
              <select className="w-full border rounded px-2 py-1 bg-white dark:bg-slate-700" value={quote} onChange={(e) => {
                const q = e.target.value; setQuote(q); if (text) { const res = parseCsv(text, delimiter, q); setParsed(res); setMapping(guessMapping(res.headers)); }
              }}>
                <option value='"'>&quot;</option>
                <option value="'">&apos;</option>
              </select>
            </div>
            {(["dateColumn","amountColumn","currencyCodeColumn","typeColumn","categoryColumn","commentColumn"] as (keyof CsvMapping)[]).map((key) => (
              <div key={key}>
                <label className="text-xs text-slate-500">{t(key as any)}</label>
                <select
                  className="w-full border rounded px-2 py-1 bg-white dark:bg-slate-700"
                  value={(mapping[key] as string) || ""}
                  onChange={(e) => setMapping((m) => ({ ...m, [key]: e.target.value || undefined }))}
                >
                  <option value="">—</option>
                  {parsed.headers.map((h) => (
                    <option key={h} value={h}>{h}</option>
                  ))}
                </select>
              </div>
            ))}
          </div>

          {mapping.categoryColumn && missingCategoryNames.length > 0 && (
            <div className="rounded-md border border-slate-200 dark:border-slate-700 p-3">
              <div className="flex items-center justify-between">
                <div className="font-medium text-sm">{t("categoryHandlingTitle")}</div>
                <label className="text-sm inline-flex items-center gap-2">
                  <input type="checkbox" checked={autoCreateMissingCategories} onChange={(e) => setAutoCreateMissingCategories(e.target.checked)} />
                  {t("createMissingCategories")}
                </label>
              </div>
              <div className="mt-2 text-xs text-slate-500">{t("noAutoCreateNote")}</div>
              <div className="mt-2 text-xs text-slate-600 dark:text-slate-300">
                {t("missingCategories")}: {missingCategoryNames.join(", ")}
              </div>
            </div>
          )}

          <div className="flex items-center justify-between">
            <Button variant="outline" icon="arrow-left" onClick={() => setStep(0)}>{tc("back") as string}</Button>
            <Button variant="primary" icon="arrow-right" iconPosition="right" onClick={() => setStep(2)}>{tc("next") as string}</Button>
          </div>
        </div>
      )}

      {/* Step 2 or 3: preview (final). If mapping required: step 3, else: step 2 */}
      {((step === 2 && !(!autoCreateMissingCategories && missingCategoryNames.length > 0)) || (step === 3 && (!autoCreateMissingCategories && missingCategoryNames.length > 0))) && parsed && (
        <div className="space-y-3">
          <div className="text-sm">
            <span className="mr-3">{t("totalRows")}: <b>{preview.total}</b></span>
            <span className="mr-3">{t("validRows")}: <b className="text-green-600">{preview.valid}</b></span>
            <span>{t("invalidRows")}: <b className="text-red-600">{preview.invalid}</b></span>
          </div>
          <div className="overflow-x-auto rounded border border-slate-200 dark:border-slate-700">
            <table className="min-w-full text-xs">
              <thead className="bg-slate-50 dark:bg-slate-700/40">
                <tr>
                  <th className="px-2 py-1 text-left">date</th>
                  <th className="px-2 py-1 text-left">amount</th>
                  <th className="px-2 py-1 text-left">currency</th>
                  <th className="px-2 py-1 text-left">type</th>
                  <th className="px-2 py-1 text-left">category</th>
                  <th className="px-2 py-1 text-left">comment</th>
                  <th className="px-2 py-1 text-left">ok</th>
                </tr>
              </thead>
              <tbody>
                {preview.sample.map((r, i) => (
                  <tr key={i} className={r.ok ? "" : "bg-red-50 dark:bg-red-900/20"}>
                    <td className="px-2 py-1">{r.date}</td>
                    <td className="px-2 py-1">{r.amount}</td>
                    <td className="px-2 py-1">{r.currency}</td>
                    <td className="px-2 py-1">{r.type}</td>
                    <td className="px-2 py-1">{r.category}</td>
                    <td className="px-2 py-1">{r.comment}</td>
                    <td className="px-2 py-1">{r.ok ? "✓" : "✗"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="flex items-center justify-between">
            <Button variant="outline" icon="arrow-left" onClick={() => {
              if (!autoCreateMissingCategories && missingCategoryNames.length > 0) setStep(2); else setStep(1);
            }}>{tc("back") as string}</Button>
            <Button variant="primary" icon="upload" loading={commitLoading} onClick={handleCommit}>
              {commitLoading ? (t("importing") as string) : (t("importNow") as string)}
            </Button>
          </div>

          {commitLoading && statusMsg && (
            <div className="text-xs text-slate-500">{statusMsg}</div>
          )}
        </div>
      )}

      {/* Step 2: category mapping when auto-create disabled and missing exist */}
      {step === 2 && ! (autoCreateMissingCategories || missingCategoryNames.length === 0) && (
        <div className="space-y-3">
          <div className="text-sm font-medium">{t("mapCategories")}</div>
          <CategoryMapping
            missingNames={missingCategoryNames}
            allCategories={allCategories || []}
            suggestedKinds={suggestedCategoryKinds}
            onQuickCreate={async (name, kind) => {
              const resp = await category.createCategory({
                kind,
                code: name,
                isActive: true,
                translations: [{ locale: locale || "ru", name }],
              } as any);
              return resp.category;
            }}
            onComplete={async (map) => {
              setManualCategoryMap(map);
              // Refresh categories list for UI consistency after creates
              try {
                await Promise.all([
                  category.listCategories({ kind: CategoryKind.INCOME, includeInactive: false } as any),
                  category.listCategories({ kind: CategoryKind.EXPENSE, includeInactive: false } as any),
                ]);
              } catch {}
              setStep(3);
            }}
          />
        </div>
      )}
    </div>
  );
}


