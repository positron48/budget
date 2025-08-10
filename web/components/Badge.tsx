import { ReactNode } from "react";

interface BadgeProps {
  children: ReactNode;
  variant?: "default" | "secondary" | "destructive" | "outline";
  className?: string;
}

export default function Badge({ children, variant = "default", className = "" }: BadgeProps) {
  const variantClasses = {
    default: "badge-default",
    secondary: "badge-secondary",
    destructive: "badge-destructive",
    outline: "badge-outline",
  };

  return (
    <span className={`badge ${variantClasses[variant]} ${className}`}>
      {children}
    </span>
  );
}
