import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import Icon from "./Icon";

interface CategoryTranslation {
  locale: string;
  name: string;
  description?: string;
}

interface CategoryItem {
  id: string;
  code: string;
  kind: number;
  translations?: CategoryTranslation[];
}

interface CategorySingleInputProps {
  categories: CategoryItem[];
  value: string | null | undefined;
  onChange: (id: string | null) => void;
  placeholder?: string;
  disabled?: boolean;
  className?: string;
}

function getCategoryDisplayName(category: CategoryItem): string {
  const translations = category.translations || [];
  const ru = translations.find(t => t.locale === "ru");
  if (ru) return ru.name;
  const en = translations.find(t => t.locale === "en");
  if (en) return en.name;
  if (translations.length > 0) return translations[0].name;
  return category.code;
}

export default function CategorySingleInput({
  categories,
  value,
  onChange,
  placeholder = "Введите код или название категории...",
  disabled = false,
  className = "",
}: CategorySingleInputProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [dropdownPosition, setDropdownPosition] = useState<{ top: number; left: number; width: number }>({ top: 0, left: 0, width: 0 });
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const pointerInDropdownRef = useRef(false);

  const selected = useMemo(() => categories.find(c => c.id === value) || null, [categories, value]);

  const filtered = useMemo(() => {
    const term = searchTerm.toLowerCase();
    return categories.filter(c =>
      getCategoryDisplayName(c).toLowerCase().includes(term) || c.code.toLowerCase().includes(term)
    );
  }, [categories, searchTerm]);

  const updateDropdownPosition = () => {
    if (!containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setDropdownPosition({ top: rect.bottom, left: rect.left, width: rect.width });
  };

  useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      const target = e.target as Node;
      const dropdown = document.querySelector('[data-dropdown="category-single-input"]');
      if (containerRef.current && !containerRef.current.contains(target) && dropdown && !dropdown.contains(target)) {
        setIsOpen(false);
      }
    };
    const onResize = () => setIsOpen(false);
    const onScroll = (e: Event) => {
      const target = e.target as Element;
      const dropdown = document.querySelector('[data-dropdown="category-single-input"]');
      if (dropdown && !dropdown.contains(target)) setIsOpen(false);
    };
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onResize);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onResize);
    };
  }, []);

  const openDropdown = () => {
    if (disabled) return;
    setIsOpen(true);
    updateDropdownPosition();
  };

  const selectCategory = (id: string) => {
    onChange(id);
    setSearchTerm("");
    setIsOpen(false);
  };

  const clearSelection = () => {
    onChange(null);
    setSearchTerm("");
    inputRef.current?.focus();
  };

  return (
    <div className={`relative ${className}`} ref={containerRef} style={{ zIndex: 9999999 }}>
      <div className={`w-full min-h-[40px] px-3 py-2 border rounded-md text-sm transition-all duration-200 ${disabled ? "opacity-60 cursor-not-allowed" : ""} bg-white dark:bg-slate-700 border-slate-200 dark:border-slate-600 text-slate-900 dark:text-white focus-within:ring-2 focus-within:ring-blue-500 focus-within:border-transparent`}> 
        <div className="flex items-center gap-2">
          <Icon name="search" size={14} className="text-slate-400" />
          <input
            ref={inputRef}
            type="text"
            className="flex-1 bg-transparent outline-none placeholder:text-slate-500 dark:placeholder:text-slate-400"
            placeholder={selected ? getCategoryDisplayName(selected) : placeholder}
            value={searchTerm}
            disabled={disabled}
            onFocus={openDropdown}
            onChange={(e) => { setSearchTerm(e.target.value); setIsOpen(true); updateDropdownPosition(); }}
            onKeyDown={(e) => {
              if (e.key === 'Tab' || e.key === 'Escape') {
                setIsOpen(false);
              }
            }}
            onBlur={() => {
              // If blur was caused by clicking inside dropdown, do not close here
              if (pointerInDropdownRef.current) return;
              setTimeout(() => setIsOpen(false), 0);
            }}
            autoComplete="off"
          />
          {selected && (
            <button type="button" onClick={clearSelection} className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300" aria-label="Очистить категорию">
              <Icon name="close" size={14} />
            </button>
          )}
        </div>
      </div>

      {isOpen && createPortal(
        <div
          data-dropdown="category-single-input"
          className="fixed bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 rounded-md shadow-xl max-h-60 overflow-hidden"
          style={{ zIndex: 9999999, top: dropdownPosition.top, left: dropdownPosition.left, width: dropdownPosition.width || undefined, minWidth: "220px" }}
          onClick={(e) => e.stopPropagation()}
          onMouseDown={() => { pointerInDropdownRef.current = true; }}
          onMouseUp={() => { setTimeout(() => { pointerInDropdownRef.current = false; }, 0); }}
        >
          <div className="max-h-60 overflow-y-auto" onScroll={(e) => e.stopPropagation()}>
            {filtered.length > 0 ? filtered.map(cat => (
              <div
                key={cat.id}
                className="px-3 py-2 cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-600 flex items-center justify-between"
                onMouseDown={(e) => { e.preventDefault(); selectCategory(cat.id); }}
              >
                <div>
                  <div className="text-sm font-medium text-slate-900 dark:text-white">{getCategoryDisplayName(cat)}</div>
                  {cat.code !== getCategoryDisplayName(cat) && (
                    <div className="text-xs text-slate-500 dark:text-slate-400">{cat.code}</div>
                  )}
                </div>
                {value === cat.id && <Icon name="check" size={14} className="text-blue-600" />}
              </div>
            )) : (
              <div className="px-3 py-2 text-sm text-slate-500 dark:text-slate-400">Категории не найдены</div>
            )}
          </div>
        </div>,
        document.body
      )}
    </div>
  );
}


