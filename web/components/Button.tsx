import { forwardRef } from "react";
import Icon from "./Icon";
import type { IconName } from "./Icon";

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "destructive" | "outline" | "ghost";
  size?: "sm" | "md" | "lg";
  icon?: IconName;
  iconPosition?: "left" | "right";
  loading?: boolean;
  children: React.ReactNode;
}

const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ 
    variant = "primary", 
    size = "md", 
    icon, 
    iconPosition = "left", 
    loading = false,
    children, 
    className = "", 
    disabled,
    ...props 
  }, ref) => {
    const baseClasses = "btn";
    const variantClasses = {
      primary: "btn-primary",
      secondary: "btn-secondary", 
      destructive: "btn-destructive",
      outline: "btn-outline",
      ghost: "btn-ghost",
    };
    const sizeClasses = {
      sm: "btn-sm",
      md: "btn-md", 
      lg: "btn-lg",
    };

    const classes = `${baseClasses} ${variantClasses[variant]} ${sizeClasses[size]} ${className}`;
    const isDisabled = disabled || loading;

    const renderIcon = () => {
      if (loading) {
        return (
          <div className="w-4 h-4 border-2 border-current/30 border-t-current rounded-full animate-spin" />
        );
      }
      if (icon) {
        return <Icon name={icon} size={size === "sm" ? 14 : 16} />;
      }
      return null;
    };

    return (
      <button
        ref={ref}
        className={classes}
        disabled={isDisabled}
        {...props}
      >
        {iconPosition === "left" && renderIcon() && (
          <span className={children ? "mr-2" : ""}>
            {renderIcon()}
          </span>
        )}
        {children}
        {iconPosition === "right" && renderIcon() && (
          <span className={children ? "ml-2" : ""}>
            {renderIcon()}
          </span>
        )}
      </button>
    );
  }
);

Button.displayName = "Button";

export default Button;
