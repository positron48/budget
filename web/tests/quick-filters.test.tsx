import React from "react";
import { screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { renderWithIntl } from "./utils";
import QuickFilters from "../components/QuickFilters";

describe('QuickFilters', () => {
  const mockOnFromChange = vi.fn();
  const mockOnToChange = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders all quick filter buttons', () => {
    renderWithIntl(
      <QuickFilters
        from=""
        to=""
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            quickFilters: "Quick filters",
            currentMonth: "Current month",
            lastMonth: "Last month",
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    expect(screen.getByText('quickFilters:')).toBeTruthy();
    expect(screen.getByText('currentMonth')).toBeTruthy();
    expect(screen.getByText('lastMonth')).toBeTruthy();
    expect(screen.getByText('currentYear')).toBeTruthy();
    expect(screen.getByText('last30Days')).toBeTruthy();
  });

  it('calls onFromChange and onToChange when current month button is clicked', () => {
    renderWithIntl(
      <QuickFilters
        from=""
        to=""
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            quickFilters: "Quick filters",
            currentMonth: "Current month",
            lastMonth: "Last month",
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const currentMonthButton = screen.getByText('currentMonth');
    fireEvent.click(currentMonthButton);

    expect(mockOnFromChange).toHaveBeenCalledTimes(1);
    expect(mockOnToChange).toHaveBeenCalledTimes(1);
    
    // Проверяем, что передаются корректные даты для текущего месяца
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), now.getMonth(), 1);
    const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    
    expect(mockOnFromChange).toHaveBeenCalledWith(firstDay.toISOString().split('T')[0]);
    expect(mockOnToChange).toHaveBeenCalledWith(lastDay.toISOString().split('T')[0]);
  });

  it('highlights current month button when dates match current month', () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), now.getMonth(), 1);
    const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    
    renderWithIntl(
      <QuickFilters
        from={firstDay.toISOString().split('T')[0]}
        to={lastDay.toISOString().split('T')[0]}
        onFromChange={mockOnFromChange}
        onToChange={mockOnToChange}
      />,
      {
        locale: "en",
        messages: {
          transactions: {
            quickFilters: "Quick filters",
            currentMonth: "Current month",
            lastMonth: "Last month",
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const currentMonthButton = screen.getByText('currentMonth');
    expect(currentMonthButton.className).toContain('btn-primary'); // primary variant
  });

  it('does not highlight current month button when dates do not match', () => {
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
            quickFilters: "Quick filters",
            currentMonth: "Current month",
            lastMonth: "Last month",
            currentYear: "Current year",
            last30Days: "Last 30 days"
          }
        }
      }
    );

    const currentMonthButton = screen.getByText('currentMonth');
    expect(currentMonthButton.className).toContain('btn-outline'); // outline variant
  });
});
