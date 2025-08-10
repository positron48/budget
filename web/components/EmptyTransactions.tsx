import Icon from "./Icon";
import Link from "next/link";

interface EmptyTransactionsProps {
  onCreateClick?: () => void;
}

export default function EmptyTransactions({ onCreateClick }: EmptyTransactionsProps) {
  return (
    <div className="text-center py-16">
      <div className="max-w-md mx-auto">
        <div className="inline-flex items-center justify-center w-24 h-24 bg-gradient-to-br from-blue-100 to-indigo-100 dark:from-blue-900/30 dark:to-indigo-900/30 rounded-full mb-6">
          <Icon name="receipt" size={48} className="text-blue-600 dark:text-blue-400" />
        </div>
        
        <h3 className="text-2xl font-bold text-slate-900 dark:text-white mb-3">
          Нет транзакций
        </h3>
        
        <p className="text-slate-600 dark:text-slate-300 text-lg mb-8 leading-relaxed">
          Начните отслеживать свои финансы, создав первую транзакцию. 
          Это поможет вам лучше понимать свои доходы и расходы.
        </p>
        
        <div className="flex flex-col sm:flex-row gap-3 justify-center">
          <Link
            href="/transactions/new"
            onClick={onCreateClick}
            className="inline-flex items-center justify-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-semibold rounded-lg shadow-lg hover:shadow-xl transition-all duration-200 transform hover:scale-105"
          >
            <Icon name="plus" size={18} className="mr-2" />
            Создать первую транзакцию
          </Link>
          
          <Link
            href="/categories"
            className="inline-flex items-center justify-center px-6 py-3 text-slate-600 dark:text-slate-300 hover:text-slate-900 dark:hover:text-white font-medium rounded-lg border border-slate-200 dark:border-slate-600 hover:bg-white dark:hover:bg-slate-800 transition-all duration-200"
          >
            <Icon name="folder-open" size={18} className="mr-2" />
            Настроить категории
          </Link>
        </div>
        
        <div className="mt-8 p-4 bg-slate-50 dark:bg-slate-800/50 rounded-lg">
          <h4 className="font-semibold text-slate-900 dark:text-white mb-2">
            💡 Советы для начала:
          </h4>
          <ul className="text-sm text-slate-600 dark:text-slate-300 space-y-1 text-left">
            <li>• Создайте категории для разных типов расходов</li>
            <li>• Регулярно добавляйте все свои транзакции</li>
            <li>• Используйте комментарии для лучшего понимания</li>
            <li>• Анализируйте отчеты для оптимизации бюджета</li>
          </ul>
        </div>
      </div>
    </div>
  );
}
