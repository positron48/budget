import { forwardRef } from "react";
import Icon from "./Icon";
import type { IconName } from "./Icon";

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  leftIcon?: IconName;
  rightIcon?: IconName;
  onRightIconClick?: () => void;
}

const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ 
    label, 
    error, 
    leftIcon, 
    rightIcon, 
    onRightIconClick,
    className = "", 
    id,
    ...props 
  }, ref) => {
    const inputId = id || `input-${Math.random().toString(36).substr(2, 9)}`;

    return (
      <div className="space-y-2">
        {label && (
          <label htmlFor={inputId} className="text-sm font-medium text-foreground">
            {label}
          </label>
        )}
        <div className="relative">
          {leftIcon && (
            <div className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground">
              <Icon name={leftIcon} size={16} />
            </div>
          )}
          <input
            ref={ref}
            id={inputId}
            className={`input ${leftIcon ? 'pl-10' : ''} ${rightIcon ? 'pr-10' : ''} ${error ? 'border-destructive' : ''} ${className}`}
            {...props}
          />
          {rightIcon && (
            <button
              type="button"
              onClick={onRightIconClick}
              className="absolute right-3 top-1/2 transform -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
              disabled={props.disabled}
            >
              <Icon name={rightIcon} size={16} />
            </button>
          )}
        </div>
        {error && (
          <p className="text-sm text-destructive flex items-center space-x-1">
            <Icon name="alert-circle" size={14} />
            <span>{error}</span>
          </p>
        )}
      </div>
    );
  }
);

Input.displayName = "Input";

export default Input;
