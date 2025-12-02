import { forwardRef, useId } from "react";
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
    const reactId = useId();
    const inputId = id || `input-${reactId}`;

    return (
      <div className="space-y-2">
        {label && (
          <label htmlFor={inputId} className="text-sm font-medium text-foreground">
            {label}
          </label>
        )}
        <div className="relative">
          <input
            ref={ref}
            id={inputId}
            className={`input ${rightIcon ? 'has-right-icon pr-12' : ''} ${error ? 'border-destructive' : ''} ${className}`}
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
