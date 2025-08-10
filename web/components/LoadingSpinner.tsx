"use client";

interface LoadingSpinnerProps {
  size?: "sm" | "md" | "lg";
  text?: string;
  className?: string;
}

export default function LoadingSpinner({ size = "md", text, className = "" }: LoadingSpinnerProps) {
  const sizeClasses = {
    sm: "w-4 h-4",
    md: "w-6 h-6",
    lg: "w-8 h-8",
  };

  return (
    <div className={`flex items-center justify-center ${className}`}>
      <div className="flex items-center space-x-2">
        <div className={`${sizeClasses[size]} border-2 border-primary/30 border-t-primary rounded-full animate-spin`}></div>
        {text && <span className="text-muted-foreground">{text}</span>}
      </div>
    </div>
  );
}
