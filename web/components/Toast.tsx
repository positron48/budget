"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import Icon, { type IconName } from "./Icon";

export type ToastVariant = "success" | "error" | "info" | "warning";

interface ToastItem {
  id: number;
  message: string;
  variant: ToastVariant;
}

interface ToastContextValue {
  toast: (message: string, variant?: ToastVariant) => void;
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
  warning: (message: string) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

const variantStyles: Record<ToastVariant, { icon: IconName; cls: string }> = {
  success: { icon: "check", cls: "border-[hsl(var(--positive))] text-[hsl(var(--positive))]" },
  error: { icon: "alert-circle", cls: "border-[hsl(var(--negative))] text-[hsl(var(--negative))]" },
  info: { icon: "info", cls: "border-[hsl(var(--info))] text-[hsl(var(--info))]" },
  warning: { icon: "alert-circle", cls: "border-[hsl(var(--warning))] text-[hsl(var(--warning))]" },
};

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const [mounted, setMounted] = useState(false);
  const idRef = useRef(0);

  useEffect(() => setMounted(true), []);

  const remove = useCallback((id: number) => {
    setItems((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback(
    (message: string, variant: ToastVariant = "info") => {
      const id = ++idRef.current;
      setItems((prev) => [...prev, { id, message, variant }]);
      setTimeout(() => remove(id), 4000);
    },
    [remove]
  );

  const value: ToastContextValue = {
    toast,
    success: (m) => toast(m, "success"),
    error: (m) => toast(m, "error"),
    info: (m) => toast(m, "info"),
    warning: (m) => toast(m, "warning"),
  };

  return (
    <ToastContext.Provider value={value}>
      {children}
      {mounted &&
        createPortal(
          <div className="fixed bottom-4 right-4 z-[10001] flex flex-col gap-2 max-w-sm w-[calc(100vw-2rem)] sm:w-auto">
            {items.map((t) => {
              const v = variantStyles[t.variant];
              return (
                <div
                  key={t.id}
                  role="status"
                  className={`flex items-start gap-2 rounded-lg border-l-4 bg-card text-card-foreground shadow-lg px-4 py-3 animate-in ${v.cls}`}
                >
                  <Icon name={v.icon} size={18} className="mt-0.5 shrink-0" />
                  <span className="text-sm text-foreground flex-1">{t.message}</span>
                  <button
                    onClick={() => remove(t.id)}
                    aria-label="Close"
                    className="text-muted-foreground hover:text-foreground shrink-0"
                  >
                    <Icon name="close" size={14} />
                  </button>
                </div>
              );
            })}
          </div>,
          document.body
        )}
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
