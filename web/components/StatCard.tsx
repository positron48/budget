"use client";

interface StatCardProps {
  icon: string;
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
    primary: "text-blue-600",
    success: "text-green-600",
    warning: "text-yellow-600",
    danger: "text-red-600",
  };

  return (
    <div className="card hover:shadow-md transition-shadow">
      <div className="card-content">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-muted-foreground">{title}</p>
            <p className="text-2xl font-bold text-foreground">{value}</p>
            {change && (
              <p className={`text-sm font-medium ${change.isPositive ? 'text-green-600' : 'text-red-600'}`}>
                {change.isPositive ? '↗' : '↘'} {Math.abs(change.value)}%
              </p>
            )}
          </div>
          <div className={`text-3xl ${colorClasses[color]}`}>
            {icon}
          </div>
        </div>
      </div>
    </div>
  );
}
