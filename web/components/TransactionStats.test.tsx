import { render, screen } from '@testing-library/react';
import TransactionStats from './TransactionStats';
import { NextIntlClientProvider } from 'next-intl';
import messages from '../i18n/ru.json';

// Мокаем next-intl
jest.mock('next-intl', () => ({
  useTranslations: () => (key: string) => {
    const translations: Record<string, string> = {
      totalIncome: 'Общий доход',
      totalExpenses: 'Общие расходы',
      netIncome: 'Чистый доход',
      profit: 'Прибыль',
      loss: 'Убыток',
      savingsPercent: 'экономия',
      spendingPercent: 'трат',
      ofIncome: 'от дохода',
      periodCurrent: 'за текущий период'
    };
    return translations[key] || key;
  }
}));

describe('TransactionStats', () => {
  const renderWithProvider = (props: any) => {
    return render(
      <NextIntlClientProvider messages={messages} locale="ru">
        <TransactionStats {...props} />
      </NextIntlClientProvider>
    );
  };

  it('should display savings percentage when net income is positive', () => {
    renderWithProvider({
      totalIncome: 10000,
      totalExpenses: 6000,
      currencyCode: 'RUB',
      period: 'за месяц'
    });

    // Проверяем, что отображается процент экономии (40% от дохода)
    expect(screen.getByText('40% экономия от дохода')).toBeInTheDocument();
  });

  it('should display spending percentage when net income is negative', () => {
    renderWithProvider({
      totalIncome: 10000,
      totalExpenses: 15000,
      currencyCode: 'RUB',
      period: 'за месяц'
    });

    // Проверяем, что отображается процент трат (50% от дохода)
    expect(screen.getByText('50% трат от дохода')).toBeInTheDocument();
  });

  it('should not display percentage when total income is zero', () => {
    renderWithProvider({
      totalIncome: 0,
      totalExpenses: 1000,
      currencyCode: 'RUB',
      period: 'за месяц'
    });

    // Проверяем, что процент не отображается
    expect(screen.queryByText(/экономия от дохода/)).not.toBeInTheDocument();
    expect(screen.queryByText(/трат от дохода/)).not.toBeInTheDocument();
  });

  it('should round percentage to one decimal place', () => {
    renderWithProvider({
      totalIncome: 10000,
      totalExpenses: 3333,
      currencyCode: 'RUB',
      period: 'за месяц'
    });

    // Проверяем, что процент округляется до 1 знака после запятой (66.7%)
    expect(screen.getByText('66.7% экономия от дохода')).toBeInTheDocument();
  });
});
