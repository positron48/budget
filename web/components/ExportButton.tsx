"use client";

import { useState } from "react";
import { Button } from "@/components";
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
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-slate-800 rounded-lg p-6 max-w-md w-full mx-4">
            <h3 className="text-lg font-semibold text-slate-900 dark:text-white mb-4">
              {t("exportTitle")}
            </h3>
            
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
                <p className="text-slate-600 dark:text-slate-300 mb-2">
                  {t("exportWillExport")} <strong>{totalCount}</strong> {t("exportTransactions")}
                </p>
                <p className="text-sm text-slate-500 dark:text-slate-400">
                  {t("exportWithFilters")}
                </p>
              </div>
            )}

            <div className="flex justify-end space-x-3">
              <Button
                variant="outline"
                onClick={handleCancelExport}
                disabled={isLoading}
              >
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
