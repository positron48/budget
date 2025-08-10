"use client";

import Icon from "./Icon";

interface LoadingSpinnerProps {
  text?: string;
  className?: string;
  size?: "sm" | "md" | "lg";
}

export default function LoadingSpinner({ text, className = "", size = "md" }: LoadingSpinnerProps) {
  const sizeClasses = {
    sm: "w-4 h-4",
    md: "w-6 h-6", 
    lg: "w-8 h-8",
  };

  const iconSizes = {
    sm: 16,
    md: 24,
    lg: 32,
  };

  return (
    <div className={`flex items-center justify-center ${className}`}>
      <div className="flex items-center space-x-3">
        <div className={`${sizeClasses[size]} border-2 border-primary/30 border-t-primary rounded-full animate-spin`}></div>
        {text && (
          <span className="text-muted-foreground flex items-center space-x-2">
            <Icon name="loader-2" size={iconSizes[size]} className="animate-spin" />
            <span>{text}</span>
          </span>
        )}
      </div>
    </div>
  );
}
