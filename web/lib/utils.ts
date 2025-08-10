export function formatCurrency(amount: number, currencyCode: string = "RUB"): string {
  const formattedAmount = new Intl.NumberFormat('ru-RU', {
    style: 'currency',
    currency: currencyCode,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount / 100);
  
  return formattedAmount;
}

export function formatAmountWithSpaces(amount: number): string {
  return new Intl.NumberFormat('ru-RU').format(amount);
}
