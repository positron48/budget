import { TransactionType } from "@/proto/budget/v1/common_pb";

export interface ExportTransaction {
  id: string;
  type: TransactionType;
  occurredAt: { seconds: number | bigint };
  comment: string;
  amount: { minorUnits: number | bigint; currencyCode: string };
  categoryId?: string;
  categoryCode?: string;
  categoryName?: string;
}

export function exportTransactionsToCsv(transactions: ExportTransaction[], locale: string = "ru"): string {
  // Заголовки CSV в зависимости от локали
  const headers = locale === "en" ? [
    "Date",
    "Type", 
    "Amount",
    "Currency",
    "Category",
    "Description"
  ] : [
    "Дата",
    "Тип", 
    "Сумма",
    "Валюта",
    "Категория",
    "Описание"
  ];

  const rows = transactions.map(tx => {
    // Конвертируем BigInt в number для работы с датой
    const seconds = typeof tx.occurredAt.seconds === 'bigint' 
      ? Number(tx.occurredAt.seconds) 
      : tx.occurredAt.seconds;
    const date = new Date(seconds * 1000);
    const formattedDate = date.toISOString().split('T')[0]; // YYYY-MM-DD
    
    const type = tx.type === TransactionType.EXPENSE 
      ? (locale === "en" ? "Expense" : "Расход") 
      : (locale === "en" ? "Income" : "Доход");
    
    // Конвертируем BigInt в number для вычисления суммы
    const minorUnits = typeof tx.amount.minorUnits === 'bigint' 
      ? Number(tx.amount.minorUnits) 
      : tx.amount.minorUnits;
    const amount = (minorUnits / 100).toFixed(2);
    
    const currency = tx.amount.currencyCode || "RUB";
    
    const category = tx.categoryName || tx.categoryCode || "";
    
    const comment = tx.comment || "";

    return [
      formattedDate,
      type,
      amount,
      currency,
      category,
      comment
    ];
  });

  // Создаем CSV строку
  const csvContent = [
    headers.join(","),
    ...rows.map(row => 
      row.map(cell => {
        // Экранируем запятые и кавычки в ячейках
        const cellStr = String(cell);
        if (cellStr.includes(',') || cellStr.includes('"') || cellStr.includes('\n')) {
          return `"${cellStr.replace(/"/g, '""')}"`;
        }
        return cellStr;
      }).join(",")
    )
  ].join("\n");

  return csvContent;
}

export function downloadCsv(csvContent: string, filename: string): void {
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
  const link = document.createElement('a');
  
  if (link.download !== undefined) {
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', filename);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  }
}
