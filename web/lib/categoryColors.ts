/**
 * Generate consistent colors for categories based on their names
 * Uses a hash function to ensure the same category always gets the same color
 */

// Predefined color palettes for different category types
const EXPENSE_COLORS = [
  "#ef4444", // red-500
  "#f59e0b", // amber-500
  "#f97316", // orange-500
  "#e11d48", // rose-600
  "#dc2626", // red-600
  "#ea580c", // orange-600
  "#b91c1c", // red-700
  "#c2410c", // orange-700
  "#be123c", // rose-700
  "#fbbf24", // amber-400
  "#f59e0b", // amber-500
  "#d97706", // amber-600
  "#92400e", // amber-800
  "#78350f", // amber-900
  "#991b1b", // red-800
  "#7f1d1d", // red-900
  "#9a3412", // orange-800
  "#7c2d12", // orange-900
  "#be185d", // pink-700
  "#a21caf", // purple-600
  "#86198f", // purple-700
  "#7c3aed", // violet-600
  "#6d28d9", // violet-700
  "#5b21b6", // violet-800
  "#4c1d95", // violet-900
];

const INCOME_COLORS = [
  "#3b82f6", // blue-500
  "#22c55e", // green-500
  "#06b6d4", // cyan-500
  "#6366f1", // indigo-500
  "#14b8a6", // teal-500
  "#0ea5e9", // sky-500
  "#10b981", // emerald-500
  "#2563eb", // blue-600
  "#1d4ed8", // blue-700
  "#1e40af", // blue-800
  "#1e3a8a", // blue-900
  "#16a34a", // green-600
  "#15803d", // green-700
  "#166534", // green-800
  "#14532d", // green-900
  "#0891b2", // cyan-600
  "#0e7490", // cyan-700
  "#155e75", // cyan-800
  "#164e63", // cyan-900
  "#4f46e5", // indigo-600
  "#4338ca", // indigo-700
  "#3730a3", // indigo-800
  "#312e81", // indigo-900
  "#0d9488", // teal-600
  "#0f766e", // teal-700
  "#115e59", // teal-800
  "#134e4a", // teal-900
  "#0284c7", // sky-600
  "#0369a1", // sky-700
  "#075985", // sky-800
  "#0c4a6e", // sky-900
  "#059669", // emerald-600
  "#047857", // emerald-700
  "#065f46", // emerald-800
  "#064e3b", // emerald-900
];

/**
 * Simple hash function for strings
 * Returns a number between 0 and 1
 */
function hashString(str: string): number {
  let hash = 0;
  if (str.length === 0) return hash;
  
  for (let i = 0; i < str.length; i++) {
    const char = str.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash; // Convert to 32-bit integer
  }
  
  // Convert to positive number between 0 and 1
  return Math.abs(hash) / 2147483647;
}

/**
 * Get a consistent color for a category based on its name
 * @param categoryName - The name of the category
 * @param type - The type of category ('expense' or 'income')
 * @returns A hex color string
 */
export function getCategoryColor(categoryName: string, type: 'expense' | 'income'): string {
  const colors = type === 'expense' ? EXPENSE_COLORS : INCOME_COLORS;
  const hash = hashString(categoryName.toLowerCase().trim());
  const index = Math.floor(hash * colors.length);
  return colors[index];
}

/**
 * Get colors for multiple categories
 * @param categories - Array of category objects with name and type
 * @returns Array of category objects with added color property
 */
export function getCategoryColors<T extends { name: string; type: 'expense' | 'income' }>(
  categories: T[]
): (T & { color: string })[] {
  return categories.map(category => ({
    ...category,
    color: getCategoryColor(category.name, category.type)
  }));
}
