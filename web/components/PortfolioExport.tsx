"use client";

import {useEffect, useRef, useState} from "react";
import {useLocale} from "next-intl";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {downloadPortfolioExport} from "@/lib/portfolio-export";
import Modal from "@/components/Modal";
import type {InvestmentExportJob} from "@/proto/budget/v1/investment_pb";

export default function PortfolioExport({tenantId}: {tenantId: string}) {
  const ru = useLocale() === "ru", tr = (r: string, e: string) => ru ? r : e;
  const {investment: api} = useClients();
  const [open, setOpen] = useState(false), [job, setJob] = useState<InvestmentExportJob>();
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [progress, setProgress] = useState(0);
  const key = useRef<string | undefined>(undefined), controller = useRef<AbortController | undefined>(undefined);
  const alive = useRef(true);
  useEffect(() => {alive.current = true; return () => {alive.current = false; controller.current?.abort();};}, []);
  const status = useQuery<{job?: InvestmentExportJob}>({queryKey: ["investment", tenantId, "export", job?.id], enabled: open && !!job && ["pending", "running", "ready"].includes(job.state),
    queryFn: async ({signal}) => {
      const response = await api.getInvestmentExport({jobId: job!.id}, {signal});
      if (response.job?.id !== job!.id || !["pending", "running", "ready", "error", "expired"].includes(response.job.state)) throw new Error("invalid_export_status");
      return response;
    }, refetchInterval: job && ["pending", "running"].includes(job.state) ? 2000 : false, retry: false});
  useEffect(() => {if (status.data?.job?.id === job?.id) setJob(status.data?.job);}, [status.data, job?.id]);
  const start = async () => {
    setBusy(true); setError("");
    key.current ||= crypto.randomUUID();
    const next = new AbortController(); controller.current = next;
    try {
      const response = await api.startInvestmentExport({requestKey: key.current}, {signal: next.signal});
      if (!response.job?.id || !["pending", "running", "ready", "error", "expired"].includes(response.job.state)) throw new Error("missing_export");
      if (alive.current && !next.signal.aborted) setJob(response.job);
    } catch {if (alive.current && !next.signal.aborted) setError(tr("Не удалось подготовить файл. Повторите попытку.", "Could not prepare the file. Please retry."));}
    finally {if (alive.current && controller.current === next) setBusy(false);}
  };
  const download = async () => {
    if (!job) return;
    setBusy(true); setError(""); setProgress(0);
    const next = new AbortController(); controller.current = next;
    try {
      const blob = await downloadPortfolioExport(api, job, next.signal, (received, total) => {if (alive.current) setProgress(Math.floor(received / total * 100));});
      if (!alive.current || next.signal.aborted) return;
      const url = URL.createObjectURL(blob), anchor = document.createElement("a");
      anchor.href = url; anchor.download = `budget-investments-${job.id}.json`; anchor.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch {if (alive.current && !next.signal.aborted) {setError(tr("Не удалось скачать и проверить файл. Повторите скачивание.", "Could not download and verify the file. Please retry the download.")); void status.refetch();}}
    finally {if (alive.current && controller.current === next) setBusy(false);}
  };
  const failed = job?.state === "error" || job?.state === "expired";
  const button = "rounded-lg border border-slate-300 px-4 py-2 text-sm dark:border-slate-700 disabled:opacity-40";
  return <>
    <button className={button} onClick={() => {setOpen(true); setError("");}}>{tr("Скачать архив", "Export archive")}</button>
    {open && <Modal open title={tr("Экспорт портфеля", "Export portfolio")} onClose={() => {controller.current?.abort(); setOpen(false); setBusy(false);}}>
      <div className="space-y-4 text-slate-700 dark:text-slate-200">
        <p>{tr("JSON-архив всех инвестиционных счетов, операций и исходных импортов. Подходит для восстановления инвестиционной истории.", "A JSON archive of all investment accounts, operations and original imports. Use it to restore investment history.")}</p>
        <p className="text-sm text-slate-600 dark:text-slate-300">{tr("Котировки, валютные курсы и связи с накоплениями требуют отдельной резервной копии Budget. Файл содержит личные данные; серверная копия доступна 24 часа.", "Quotes, exchange rates and savings links require a separate Budget backup. The file contains personal data; the server copy is available for 24 hours.")}</p>
        {(error || status.error || failed) && <p role="alert">{error || (failed ? tr("Архив недоступен. Подготовьте новый файл.", "The archive is unavailable. Prepare a new file.") : tr("Не удалось проверить готовность. Повторите проверку.", "Could not check readiness. Please retry."))}</p>}
        {job && ["pending", "running"].includes(job.state) && <p role="status">{tr("Готовим архив… Можно закрыть окно и вернуться к скачиванию позже.", "Preparing the archive… You can close this window and return to download it later.")}</p>}
        {busy && job?.state === "ready" && <p role="status">{tr("Скачивание", "Downloading")}: {progress}%</p>}
        {status.error && <button className={button} onClick={() => void status.refetch()}>{tr("Проверить снова", "Check again")}</button>}
        {(!job || failed) && <button className={button} disabled={busy} onClick={() => {if (failed) key.current = undefined; void start();}}>{tr("Подготовить архив", "Prepare archive")}</button>}
        {job?.state === "ready" && <button className={button} disabled={busy} onClick={() => void download()}>{tr("Скачать JSON", "Download JSON")}</button>}
      </div>
    </Modal>}
  </>;
}
