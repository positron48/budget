import { ReactNode } from "react";

interface CardProps {
  children: ReactNode;
  className?: string;
  hover?: boolean;
}

interface CardHeaderProps {
  children: ReactNode;
  className?: string;
}

interface CardContentProps {
  children: ReactNode;
  className?: string;
}

interface CardFooterProps {
  children: ReactNode;
  className?: string;
}

export function Card({ children, className = "", hover = false }: CardProps) {
  return (
    <div className={`card ${hover ? 'hover:shadow-md transition-shadow' : ''} ${className}`}>
      {children}
    </div>
  );
}

export function CardHeader({ children, className = "" }: CardHeaderProps) {
  return (
    <div className={`card-header ${className}`}>
      {children}
    </div>
  );
}

export function CardContent({ children, className = "" }: CardContentProps) {
  return (
    <div className={`card-content ${className}`}>
      {children}
    </div>
  );
}

export function CardFooter({ children, className = "" }: CardFooterProps) {
  return (
    <div className={`card-footer ${className}`}>
      {children}
    </div>
  );
}

export function CardTitle({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <h3 className={`card-title ${className}`}>
      {children}
    </h3>
  );
}

export function CardDescription({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <p className={`card-description ${className}`}>
      {children}
    </p>
  );
}
