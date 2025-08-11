"use client";

import { useEffect } from "react";

interface ModalProps {
  open: boolean;
  title?: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  maxWidthClass?: string; // e.g. max-w-lg
}

export default function Modal({ open, title, onClose, children, footer, maxWidthClass = "max-w-lg" }: ModalProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-3">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className={`relative w-full ${maxWidthClass} bg-white dark:bg-slate-800 rounded-lg shadow-xl border border-slate-200 dark:border-slate-700`}> 
        <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700">
          <h3 className="text-base font-semibold text-slate-900 dark:text-white">{title}</h3>
        </div>
        <div className="px-4 py-3">
          {children}
        </div>
        {footer && (
          <div className="px-4 py-3 border-t border-slate-200 dark:border-slate-700 bg-slate-50/50 dark:bg-slate-700/30">
            <div className="flex items-center justify-end gap-2">{footer}</div>
          </div>
        )}
      </div>
    </div>
  );
}


