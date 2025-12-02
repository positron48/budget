import { useState, useRef, useEffect } from "react";
import { createPortal } from "react-dom";
import Icon from "./Icon";
import { useTranslations } from "next-intl";

interface Category {
  id: string;
  code: string;
  kind: number; // CategoryKind enum value
  translations?: Array<{ locale: string; name: string; description?: string }>;
}

interface CategoryTagInputProps {
  categories: Category[];
  selectedIds: string[];
  onSelectionChange: (ids: string[]) => void;
  placeholder?: string;
  className?: string;
}

export default function CategoryTagInput({
  categories,
  selectedIds,
  onSelectionChange,
  placeholder,
  className = ""
}: CategoryTagInputProps) {
  const t = useTranslations("transactions");
  const [isOpen, setIsOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [dropdownPosition, setDropdownPosition] = useState({ top: 0, left: 0, width: 0 });
  const dropdownRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Закрываем dropdown при клике вне его и при прокрутке страницы
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      const dropdownElement = document.querySelector('[data-dropdown="category-tag-input"]');
      
      if (dropdownRef.current && 
          !dropdownRef.current.contains(target) && 
          dropdownElement && 
          !dropdownElement.contains(target)) {
        setIsOpen(false);
      }
    };

    const handleScroll = (event: Event) => {
      // Проверяем, что скролл происходит не внутри выпадающего списка
      const target = event.target as Element;
      const dropdownElement = document.querySelector('[data-dropdown="category-tag-input"]');
      
      if (dropdownElement && !dropdownElement.contains(target)) {
        setIsOpen(false);
      }
    };

    const handleResize = () => {
      setIsOpen(false);
    };

    document.addEventListener("mousedown", handleClickOutside);
    document.addEventListener("scroll", handleScroll, true);
    window.addEventListener("resize", handleResize);
    
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("scroll", handleScroll, true);
      window.removeEventListener("resize", handleResize);
    };
  }, []);

  const getCategoryName = (cat: Category) => {
    if (cat.translations) {
      const ruTranslation = cat.translations.find(t => t.locale === 'ru');
      if (ruTranslation) return ruTranslation.name;
      const enTranslation = cat.translations.find(t => t.locale === 'en');
      if (enTranslation) return enTranslation.name;
      if (cat.translations.length > 0) return cat.translations[0].name;
    }
    return cat.code;
  };

  const filteredCategories = categories.filter(cat =>
    !selectedIds.includes(cat.id) && (
      getCategoryName(cat).toLowerCase().includes(searchTerm.toLowerCase()) ||
      cat.code.toLowerCase().includes(searchTerm.toLowerCase())
    )
  );

  const selectedCategories = categories.filter(cat => selectedIds.includes(cat.id));
  const resolvedPlaceholder = placeholder ?? t("categoryIdsPlaceholder");

  const addCategory = (categoryId: string) => {
    if (!selectedIds.includes(categoryId)) {
      onSelectionChange([...selectedIds, categoryId]);
    }
    setInputValue("");
    setSearchTerm("");
    setIsOpen(false);
    inputRef.current?.focus();
  };

  const removeCategory = (categoryId: string) => {
    onSelectionChange(selectedIds.filter(id => id !== categoryId));
  };

  const updateDropdownPosition = () => {
    if (dropdownRef.current) {
      const rect = dropdownRef.current.getBoundingClientRect();
      setDropdownPosition({
        top: rect.bottom,
        left: rect.left,
        width: rect.width
      });
    }
  };

  const handleInputChange = (value: string) => {
    setInputValue(value);
    setSearchTerm(value);
    setIsOpen(true);
    updateDropdownPosition();
  };

  const handleInputKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && filteredCategories.length > 0) {
      e.preventDefault();
      addCategory(filteredCategories[0].id);
    } else if (e.key === 'Backspace' && inputValue === '' && selectedIds.length > 0) {
      e.preventDefault();
      removeCategory(selectedIds[selectedIds.length - 1]);
    }
  };

  return (
    <div className={`relative ${className}`} ref={dropdownRef} style={{ zIndex: 9999999 }}>
      <div className="w-full min-h-[40px] px-3 py-2 border border-border rounded-none bg-secondary/60 text-foreground focus-within:ring-2 focus-within:ring-primary/40 focus-within:border-transparent transition-all duration-200 text-sm">
        <div className="flex items-center flex-wrap gap-1">
          {selectedCategories.map(cat => (
            <span
              key={cat.id}
              className="inline-flex items-center px-2 py-1 text-xs font-medium bg-[hsl(var(--primary)/0.15)] text-[hsl(var(--primary))]"
            >
              {getCategoryName(cat)}
              <button
                onClick={() => removeCategory(cat.id)}
                className="ml-1 hover:text-foreground w-4 h-4 flex items-center justify-center"
              >
                <Icon name="close" size={10} />
              </button>
            </span>
          ))}
          <input
            ref={inputRef}
            type="text"
            value={inputValue}
            onChange={(e) => handleInputChange(e.target.value)}
            onKeyDown={handleInputKeyDown}
            onFocus={() => {
              setIsOpen(true);
              updateDropdownPosition();
            }}
            placeholder={selectedCategories.length === 0 ? resolvedPlaceholder : ""}
            className="flex-1 min-w-[120px] bg-transparent outline-none placeholder:text-muted-foreground"
            autoComplete="off"
          />
        </div>
      </div>

      {isOpen && createPortal(
        <div
          data-dropdown="category-tag-input"
          className="fixed border border-border shadow-xl max-h-60 overflow-hidden rounded-none backdrop-blur supports-[backdrop-filter]:bg-card/70"
          style={{
            backgroundColor: "hsl(var(--card) / 0.95)",
            zIndex: 9999999,
            top: dropdownPosition.top,
            left: dropdownPosition.left,
            width: dropdownPosition.width || "auto",
            minWidth: "200px",
          }}
          onClick={(e) => e.stopPropagation()}
        >
          <div 
            className="max-h-60 overflow-y-auto"
            onScroll={(e) => e.stopPropagation()}
          >
            {filteredCategories.length > 0 ? (
              filteredCategories.map(cat => (
                <div
                  key={cat.id}
                  className="px-3 py-2 cursor-pointer hover:bg-secondary/40 flex items-center space-x-2"
                  onClick={() => addCategory(cat.id)}
                >
                  <div className="flex-1">
                    <div className="text-sm font-medium text-foreground">
                      {getCategoryName(cat)}
                    </div>
                    {cat.code !== getCategoryName(cat) && (
                      <div className="text-xs text-muted-foreground">
                        {cat.code}
                      </div>
                    )}
                  </div>
                  <Icon name="plus" size={14} className="text-muted-foreground" />
                </div>
              ))
            ) : (
              <div className="px-3 py-2 text-sm text-muted-foreground">
                {searchTerm ? t("categoriesNotFound") : t("categoriesAllSelected")}
              </div>
            )}
          </div>
        </div>,
        document.body
      )}
    </div>
  );
}
