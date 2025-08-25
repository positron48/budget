import React from "react";
import { screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { renderWithIntl } from "./utils";
import QuickFilters from "../components/QuickFilters";
import { formatDateLocal } from "../lib/utils";

describe('QuickFilters', () => {
  const mockOnFromChange = vi.fn();
  const mockOnToChange = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders MonthYearPicker and quick filter buttons', () => {
    renderWithIntl(
      <QuickFilters
        from="2025-08-01"
        to="2025-08-31"
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    // Проверяем наличие кнопок быстрых фильтров
    expect(screen.getByText('currentYear')).toBeTruthy();
    expect(screen.getByText('last30Days')).toBeTruthy();
  });

  it('calls onFromChange and onToChange when current year button is clicked', () => {
    renderWithIntl(
      <QuickFilters
        from="2025-08-01"
        to="2025-08-31"
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const currentYearButton = screen.getByText('currentYear');
    fireEvent.click(currentYearButton);

    expect(mockOnFromChange).toHaveBeenCalledTimes(1);
    expect(mockOnToChange).toHaveBeenCalledTimes(1);
    
    // Проверяем, что передаются корректные даты для текущего года
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), 0, 1);
    const lastDay = new Date(now.getFullYear(), 11, 31);
    
    expect(mockOnFromChange).toHaveBeenCalledWith(formatDateLocal(firstDay));
    expect(mockOnToChange).toHaveBeenCalledWith(formatDateLocal(lastDay));
  });

  it('calls onFromChange and onToChange when last 30 days button is clicked', () => {
    renderWithIntl(
      <QuickFilters
        from="2025-08-01"
        to="2025-08-31"
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const last30DaysButton = screen.getByText('last30Days');
    fireEvent.click(last30DaysButton);

    expect(mockOnFromChange).toHaveBeenCalledTimes(1);
    expect(mockOnToChange).toHaveBeenCalledTimes(1);
    
    // Проверяем, что передаются корректные даты для последних 30 дней
    const now = new Date();
    const thirtyDaysAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
    
    expect(mockOnFromChange).toHaveBeenCalledWith(formatDateLocal(thirtyDaysAgo));
    expect(mockOnToChange).toHaveBeenCalledWith(formatDateLocal(now));
  });

  it('highlights current year button when dates match current year', () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), 0, 1);
    const lastDay = new Date(now.getFullYear(), 11, 31);
    
    renderWithIntl(
      <QuickFilters
        from={formatDateLocal(firstDay)}
        to={formatDateLocal(lastDay)}
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const currentYearButton = screen.getByText('currentYear');
    expect(currentYearButton.className).toContain('btn-primary'); // primary variant
  });

  it('does not highlight current year button when dates do not match', () => {
    renderWithIntl(
      <QuickFilters
        from="2023-01-01"
        to="2023-01-31"
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const currentYearButton = screen.getByText('currentYear');
    expect(currentYearButton.className).toContain('btn-outline'); // outline variant
  });
});
