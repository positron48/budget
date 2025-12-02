"use client";

import { useState } from "react";
import { Button, Icon } from "@/components";
import { useExportTransactions } from "@/lib/useExportTransactions";
import { exportTransactionsToCsv, downloadCsv } from "@/lib/exportUtils";
import { useTranslations, useLocale } from "next-intl";

interface ExportButtonProps {
  type: number;
  from: string;
  to: string;
  search: string;
  selectedCategoryIds: string[];
  disabled?: boolean;
}

export default function ExportButton({
  type,
  from,
  to,
  search,
  selectedCategoryIds,
  disabled = false
}: ExportButtonProps) {
  const t = useTranslations("transactions");
  const locale = useLocale();
  const [isExporting, setIsExporting] = useState(false);
  const [showExportModal, setShowExportModal] = useState(false);

  const {
    transactions,
    isLoading,
    error,
    totalCount
  } = useExportTransactions({
    type,
    from,
    to,
    search,
    selectedCategoryIds,
    enabled: showExportModal,
    locale
  });

  const handleExport = async () => {
    setIsExporting(true);
    setShowExportModal(true);
  };

  const handleConfirmExport = () => {
    if (transactions.length === 0) {
      alert("Нет транзакций для экспорта");
      setShowExportModal(false);
      setIsExporting(false);
      return;
    }

    try {
      const csvContent = exportTransactionsToCsv(transactions, locale);
      
      // Генерируем имя файла с текущей датой
      const now = new Date();
      const dateStr = now.toISOString().split('T')[0];
      const filename = `transactions_${dateStr}.csv`;
      
      downloadCsv(csvContent, filename);
      
      setShowExportModal(false);
      setIsExporting(false);
    } catch (error) {
      console.error('Export error:', error);
      alert('Ошибка при экспорте: ' + (error as Error).message);
      setShowExportModal(false);
      setIsExporting(false);
    }
  };

  const handleCancelExport = () => {
    setShowExportModal(false);
    setIsExporting(false);
  };

  return (
    <>
      <Button
        variant="outline"
        icon="download"
        onClick={handleExport}
        disabled={disabled || isExporting}
        loading={isExporting}
      >
        {isExporting ? t("exportPreparing") : t("export")}
      </Button>

      {/* Модальное окно подтверждения экспорта */}
      {showExportModal && (
        <div className="fixed inset-0 z-[10000] flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={handleCancelExport} />
          <div className="relative w-full max-w-md rounded-none border border-border bg-card text-foreground shadow-2xl">
            <div className="px-5 py-4 border-b border-border flex items-center justify-between">
              <h3 className="text-lg font-semibold">{t("exportTitle")}</h3>
              <button
                onClick={handleCancelExport}
                aria-label="Close"
                className="inline-flex items-center justify-center w-8 h-8 text-muted-foreground hover:text-foreground"
                disabled={isLoading}
              >
                <Icon name="close" size={16} />
              </button>
            </div>
            <div className="px-5 py-4">
            
            {isLoading && (
              <div className="flex items-center justify-center py-4">
                <div className="w-6 h-6 border-2 border-blue-200 border-t-blue-600 rounded-full animate-spin mr-3"></div>
                <span className="text-slate-600 dark:text-slate-300">
                  {t("exportLoading")}
                </span>
              </div>
            )}

            {error && (
              <div className="text-red-600 dark:text-red-400 mb-4">
                {t("exportError")}: {(error as Error).message}
              </div>
            )}

            {!isLoading && !error && (
              <div className="mb-4">
                <p className="text-muted-foreground mb-2">
                  {t("exportWillExport")} <strong>{totalCount}</strong> {t("exportTransactions")}
                </p>
                <p className="text-sm text-muted-foreground">{t("exportWithFilters")}</p>
              </div>
            )}
            </div>
            <div className="px-5 py-4 border-t border-border flex justify-end gap-3 bg-card">
              <Button variant="outline" onClick={handleCancelExport} disabled={isLoading}>
                {t("exportCancel")}
              </Button>
              <Button
                variant="primary"
                onClick={handleConfirmExport}
                disabled={isLoading || !!error || totalCount === 0}
                loading={isLoading}
              >
                {t("exportConfirm")}
              </Button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
