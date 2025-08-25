import { describe, it, expect } from "vitest";
import { formatCurrency, formatDateLocal } from "@/lib/utils";

describe('Utils', () => {
  describe('formatCurrency', () => {
    it('formats positive amounts correctly', () => {
      expect(formatCurrency(100000)).toBe('1 000,00 ₽'); // 100000 копеек = 1000 рублей
      expect(formatCurrency(123456700)).toBe('1 234 567,00 ₽'); // 123456700 копеек = 1234567 рублей
      expect(formatCurrency(0)).toBe('0,00 ₽');
    });

    it('formats negative amounts correctly', () => {
      expect(formatCurrency(-100000)).toBe('-1 000,00 ₽'); // -100000 копеек = -1000 рублей
      expect(formatCurrency(-123456700)).toBe('-1 234 567,00 ₽'); // -123456700 копеек = -1234567 рублей
    });

    it('formats decimal amounts correctly', () => {
      expect(formatCurrency(100050)).toBe('1 000,50 ₽'); // 100050 копеек = 1000.50 рублей
      expect(formatCurrency(123456)).toBe('1 234,56 ₽'); // 123456 копеек = 1234.56 рублей
    });

    it('handles very large numbers', () => {
      expect(formatCurrency(99999999900)).toBe('999 999 999,00 ₽'); // 99999999900 копеек = 999999999 рублей
    });
  });

  describe('formatDateLocal', () => {
    it('formats date correctly', () => {
      const date = new Date(2025, 7, 25); // Август 25, 2025 (локальное время)
      expect(formatDateLocal(date)).toBe('2025-08-25');
    });

    it('handles different dates', () => {
      const date1 = new Date(2025, 0, 1); // Январь 1, 2025
      expect(formatDateLocal(date1)).toBe('2025-01-01');

      const date2 = new Date(2025, 11, 31); // Декабрь 31, 2025
      expect(formatDateLocal(date2)).toBe('2025-12-31');
    });

    it('handles single digit months and days', () => {
      const date = new Date(2025, 2, 5); // Март 5, 2025
      expect(formatDateLocal(date)).toBe('2025-03-05');
    });

    it('handles edge cases', () => {
      const date = new Date(2024, 1, 29); // Февраль 29, 2024 (високосный год)
      expect(formatDateLocal(date)).toBe('2024-02-29');
    });
  });
});
