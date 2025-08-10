"use client";

import Icon from "./Icon";
import type { IconName } from "./Icon";

interface StatCardProps {
  icon: IconName;
  title: string;
  value: string | number;
  change?: {
    value: number;
    isPositive: boolean;
  };
  color?: "primary" | "success" | "warning" | "danger";
}

export default function StatCard({ icon, title, value, change, color = "primary" }: StatCardProps) {
  const colorClasses = {
    primary: "text-blue-600 bg-blue-50 dark:bg-blue-950/20",
    success: "text-green-600 bg-green-50 dark:bg-green-950/20",
    warning: "text-yellow-600 bg-yellow-50 dark:bg-yellow-950/20",
    danger: "text-red-600 bg-red-50 dark:bg-red-950/20",
  };

  return (
    <div className="card hover:shadow-md transition-shadow">
      <div className="card-content">
        <div className="flex items-center justify-between">
          <div className="flex-1">
            <p className="text-sm font-medium text-muted-foreground mb-1">{title}</p>
            <p className="text-2xl font-bold text-foreground mb-1">{value}</p>
            {change && (
              <p className={`text-sm font-medium flex items-center space-x-1 ${
                change.isPositive ? 'text-green-600' : 'text-red-600'
              }`}>
                <Icon 
                  name={change.isPositive ? "trending-up" : "trending-down"} 
                  size={14} 
                />
                <span>{Math.abs(change.value)}%</span>
              </p>
            )}
          </div>
          <div className={`p-3 rounded-lg ${colorClasses[color]}`}>
            <Icon name={icon} size={24} />
          </div>
        </div>
      </div>
    </div>
  );
}
