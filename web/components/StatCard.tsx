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
    primary: "text-[hsl(var(--primary))] bg-[hsl(var(--primary)/0.12)]",
    success: "text-[hsl(var(--positive))] bg-[hsl(var(--positive)/0.12)]",
    warning: "text-[hsl(var(--warning))] bg-[hsl(var(--warning)/0.12)]",
    danger: "text-[hsl(var(--negative))] bg-[hsl(var(--negative)/0.12)]",
  };

  return (
    <div className="card hover:shadow-md transition-shadow">
      <div className="card-content flex h-full min-h-28 items-center">
        <div className="flex w-full items-center justify-between gap-4">
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-muted-foreground mb-1">{title}</p>
            <p className="text-2xl font-bold text-foreground break-words tabular-nums">{value}</p>
            {change && (
              <p className={`text-sm font-medium flex items-center space-x-1 ${
                change.isPositive ? 'text-[hsl(var(--positive))]' : 'text-[hsl(var(--negative))]'
              }`}>
                <Icon 
                  name={change.isPositive ? "trending-up" : "trending-down"} 
                  size={14} 
                />
                <span>{Math.abs(change.value)}%</span>
              </p>
            )}
          </div>
          <div className={`shrink-0 p-3 rounded-lg ${colorClasses[color]}`}>
            <Icon name={icon} size={24} />
          </div>
        </div>
      </div>
    </div>
  );
}
