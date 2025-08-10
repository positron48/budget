import { useState, useRef, useEffect } from "react";
import Icon from "./Icon";

interface Category {
  id: string;
  name: string;
  code: string;
  kind: number;
}

interface CategorySelectProps {
  categories: Category[];
  selectedIds: string[];
  onSelectionChange: (ids: string[]) => void;
  placeholder?: string;
  className?: string;
}

export default function CategorySelect({
  categories,
  selectedIds,
  onSelectionChange,
  placeholder = "Выберите категории...",
  className = ""
}: CategorySelectProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Закрываем dropdown при клике вне его
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const filteredCategories = categories.filter(cat =>
    cat.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
    cat.code.toLowerCase().includes(searchTerm.toLowerCase())
  );

  const selectedCategories = categories.filter(cat => selectedIds.includes(cat.id));

  const toggleCategory = (categoryId: string) => {
    const newSelection = selectedIds.includes(categoryId)
      ? selectedIds.filter(id => id !== categoryId)
      : [...selectedIds, categoryId];
    onSelectionChange(newSelection);
  };

  const removeCategory = (categoryId: string) => {
    onSelectionChange(selectedIds.filter(id => id !== categoryId));
  };

  return (
    <div className={`relative ${className}`} ref={dropdownRef}>
      <div
        className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm cursor-pointer min-h-[40px] flex items-center flex-wrap gap-1"
        onClick={() => setIsOpen(!isOpen)}
      >
        {selectedCategories.length > 0 ? (
          <>
            {selectedCategories.map(cat => (
              <span
                key={cat.id}
                className="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300"
              >
                {cat.code}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    removeCategory(cat.id);
                  }}
                  className="ml-1 hover:bg-blue-200 dark:hover:bg-blue-800 rounded-full w-4 h-4 flex items-center justify-center"
                >
                  <Icon name="close" size={10} />
                </button>
              </span>
            ))}
          </>
        ) : (
          <span className="text-slate-500 dark:text-slate-400">{placeholder}</span>
        )}
        <Icon 
          name={isOpen ? "chevron-up" : "chevron-down"} 
          size={14} 
          className="ml-auto text-slate-400" 
        />
      </div>

      {isOpen && (
        <div className="absolute z-[9999] w-full mt-1 bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 rounded-md shadow-lg max-h-60 overflow-hidden">
          <div className="p-2 border-b border-slate-200 dark:border-slate-600">
            <div className="relative">
              <Icon 
                name="search" 
                size={14} 
                className="absolute left-2 top-1/2 transform -translate-y-1/2 text-slate-400" 
              />
              <input
                type="text"
                placeholder="Поиск категорий..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="w-full pl-8 pr-3 py-2 border border-slate-200 dark:border-slate-600 rounded bg-white dark:bg-slate-800 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent text-sm"
                onClick={(e) => e.stopPropagation()}
              />
            </div>
          </div>
          
          <div className="max-h-48 overflow-y-auto">
            {filteredCategories.length > 0 ? (
              filteredCategories.map(cat => (
                <div
                  key={cat.id}
                  className={`px-3 py-2 cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-600 flex items-center space-x-2 ${
                    selectedIds.includes(cat.id) ? 'bg-blue-50 dark:bg-blue-900/20' : ''
                  }`}
                  onClick={() => toggleCategory(cat.id)}
                >
                  <div className={`w-4 h-4 border rounded flex items-center justify-center ${
                    selectedIds.includes(cat.id) 
                      ? 'bg-blue-600 border-blue-600' 
                      : 'border-slate-300 dark:border-slate-500'
                  }`}>
                    {selectedIds.includes(cat.id) && (
                      <Icon name="check" size={12} className="text-white" />
                    )}
                  </div>
                  <div className="flex-1">
                    <div className="text-sm font-medium text-slate-900 dark:text-white">
                      {cat.code}
                    </div>
                    <div className="text-xs text-slate-500 dark:text-slate-400">
                      {cat.name}
                    </div>
                  </div>
                </div>
              ))
            ) : (
              <div className="px-3 py-2 text-sm text-slate-500 dark:text-slate-400">
                Категории не найдены
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
