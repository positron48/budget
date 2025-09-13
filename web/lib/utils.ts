import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

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

/**
 * Форматирует дату в формат YYYY-MM-DD без учета часового пояса
 * Используется для быстрых фильтров и других случаев, где нужно локальное представление даты
 */
export const formatDateLocal = (date: Date): string => {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
};
