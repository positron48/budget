import Icon from "./Icon";

interface CategoryBadgeProps {
  categoryId?: string;
  categoryName?: string;
  categoryCode?: string;
  categoryTranslations?: Array<{ locale: string; name: string; description?: string }>;
  type: 'expense' | 'income';
  className?: string;
}

export default function CategoryBadge({ 
  categoryId, 
  categoryName, 
  categoryCode, 
  categoryTranslations,
  type,
  className = "" 
}: CategoryBadgeProps) {
  if (!categoryId && !categoryName && !categoryCode) {
    return (
      <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300 ${className}`}>
        <Icon name="tag" size={10} className="mr-1" />
        Без категории
      </span>
    );
  }

  const getCategoryColor = () => {
    const colors = {
      expense: {
        bg: 'bg-red-100 dark:bg-red-900/30',
        text: 'text-red-700 dark:text-red-300',
        border: 'border-red-200 dark:border-red-800'
      },
      income: {
        bg: 'bg-green-100 dark:bg-green-900/30',
        text: 'text-green-700 dark:text-green-300',
        border: 'border-green-200 dark:border-green-800'
      }
    };
    return colors[type];
  };

  const colors = getCategoryColor();

  // Получаем название категории из переводов или используем переданное название
  const getCategoryName = () => {
    if (categoryName) return categoryName;
    if (categoryTranslations) {
      const ruTranslation = categoryTranslations.find(t => t.locale === 'ru');
      if (ruTranslation) return ruTranslation.name;
      const enTranslation = categoryTranslations.find(t => t.locale === 'en');
      if (enTranslation) return enTranslation.name;
      if (categoryTranslations.length > 0) return categoryTranslations[0].name;
    }
    return null;
  };

  const displayName = categoryCode || getCategoryName() || "Без категории";

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium border ${colors.bg} ${colors.text} ${colors.border} ${className}`}>
      <Icon name="tag" size={10} className="mr-1" />
      {displayName}
    </span>
  );
}
