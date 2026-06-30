"use client";

import { useState } from "react";
import { Button, Modal, useToast } from "@/components";
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
  const toast = useToast();
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
      toast.warning(t("exportNoTransactions"));
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
      toast.success(t("exportDone"));

      setShowExportModal(false);
      setIsExporting(false);
    } catch (error) {
      toast.error(`${t("exportError")}: ${(error as Error).message}`);
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
      <Modal
        open={showExportModal}
        onClose={handleCancelExport}
        title={t("exportTitle")}
        maxWidthClass="max-w-md"
        footer={
          <>
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
          </>
        }
      >
        {isLoading && (
          <div className="flex items-center justify-center py-4">
            <div className="w-6 h-6 border-2 border-primary/30 border-t-primary rounded-full animate-spin mr-3"></div>
            <span className="text-muted-foreground">{t("exportLoading")}</span>
          </div>
        )}

        {error && (
          <div className="text-[hsl(var(--negative))] mb-4">
            {t("exportError")}: {(error as Error).message}
          </div>
        )}

        {!isLoading && !error && (
          <div>
            <p className="text-muted-foreground mb-2">
              {t("exportWillExport")} <strong>{totalCount}</strong> {t("exportTransactions")}
            </p>
            <p className="text-sm text-muted-foreground">{t("exportWithFilters")}</p>
          </div>
        )}
      </Modal>
    </>
  );
}
