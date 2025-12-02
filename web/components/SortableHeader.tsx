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
      className={`px-4 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:bg-secondary/40 transition-colors duration-200 ${className}`}
      onClick={handleClick}
    >
      <div className="flex items-center space-x-1">
        <span>{children}</span>
        <div className="flex flex-col">
          {currentDirection === "asc" ? (
            <Icon name="chevron-up" size={12} className="text-[hsl(var(--primary))]" />
          ) : currentDirection === "desc" ? (
            <Icon name="chevron-down" size={12} className="text-[hsl(var(--primary))]" />
          ) : (
            <div className="flex flex-col -space-y-1">
              <Icon name="chevron-up" size={10} className="text-muted-foreground" />
              <Icon name="chevron-down" size={10} className="text-muted-foreground" />
            </div>
          )}
        </div>
      </div>
    </th>
  );
}
