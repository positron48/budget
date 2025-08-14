import Icon from "./Icon";

export type SortDirection = "asc" | "desc" | null;

interface SortableHeaderProps {
  children: React.ReactNode;
  field: string;
  currentSort: string;
  onSort: (field: string, direction: SortDirection) => void;
  className?: string;
  defaultDirection?: SortDirection; // Direction for first click
}

export function SortableHeader({ 
  children, 
  field, 
  currentSort, 
  onSort, 
  className = "",
  defaultDirection = "desc" // Default fallback
}: SortableHeaderProps) {
  const getCurrentDirection = (): SortDirection => {
    if (currentSort === `${field} asc`) return "asc";
    if (currentSort === `${field} desc`) return "desc";
    return null;
  };

  const handleClick = () => {
    const currentDirection = getCurrentDirection();
    let newDirection: SortDirection;
    
    if (currentDirection === null) {
      // First click: use field-specific default direction
      newDirection = defaultDirection;
    } else if (currentDirection === defaultDirection) {
      // Second click: switch to opposite direction
      newDirection = defaultDirection === "asc" ? "desc" : "asc";
    } else {
      // Third click: remove sorting (reset to default sort)
      newDirection = null;
    }
    
    onSort(field, newDirection);
  };

  const currentDirection = getCurrentDirection();

  return (
    <th 
      className={`px-4 py-3 text-left text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider cursor-pointer hover:bg-slate-100 dark:hover:bg-slate-600/50 transition-colors duration-200 ${className}`}
      onClick={handleClick}
    >
      <div className="flex items-center space-x-1">
        <span>{children}</span>
        <div className="flex flex-col">
          {currentDirection === "asc" ? (
            <Icon name="chevron-up" size={12} className="text-blue-600 dark:text-blue-400" />
          ) : currentDirection === "desc" ? (
            <Icon name="chevron-down" size={12} className="text-blue-600 dark:text-blue-400" />
          ) : (
            <div className="flex flex-col -space-y-1">
              <Icon name="chevron-up" size={10} className="text-slate-400 dark:text-slate-500" />
              <Icon name="chevron-down" size={10} className="text-slate-400 dark:text-slate-500" />
            </div>
          )}
        </div>
      </div>
    </th>
  );
}
