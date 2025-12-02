import Icon from "./Icon";
import { useTranslations } from "next-intl";

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
  const t = useTranslations("transactions");
  if (!categoryId && !categoryName && !categoryCode) {
    return (
      <span className={`inline-flex items-center px-2 py-0.5 text-xs font-medium text-muted-foreground rounded-none ${className}`}>
        <Icon name="tag" size={10} className="mr-1" />
        {t("noCategory")}
      </span>
    );
  }

  const colors =
    type === "expense"
      ? {
          bg: "bg-[hsl(var(--negative)/0.15)]",
          text: "text-[hsl(var(--negative))]",
          border: "border-[hsl(var(--negative)/0.3)]",
        }
      : {
          bg: "bg-[hsl(var(--positive)/0.15)]",
          text: "text-[hsl(var(--positive))]",
          border: "border-[hsl(var(--positive)/0.3)]",
        };

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

  const displayName = categoryCode || getCategoryName() || t("noCategory");

  return (
    <span className={`inline-flex items-center px-2 py-0.5 text-xs font-medium border rounded-none ${colors.bg} ${colors.text} ${colors.border} ${className}`}>
      <Icon name="tag" size={10} className="mr-1" />
      {displayName}
    </span>
  );
}
