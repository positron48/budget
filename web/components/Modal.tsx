"use client";

import { useEffect } from "react";
import Icon from "./Icon";

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
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-3">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm animate-in" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        className={`relative w-full ${maxWidthClass} bg-white dark:bg-slate-800 rounded-xl shadow-2xl ring-1 ring-black/10 dark:ring-white/10 overflow-hidden animate-in`}
      >
        <div className="px-4 sm:px-5 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between bg-gradient-to-b from-white/90 to-white dark:from-slate-800/90 dark:to-slate-800">
          <h3 className="text-lg font-semibold text-slate-900 dark:text-white">{title}</h3>
          <button
            onClick={onClose}
            aria-label="Close"
            className="inline-flex items-center justify-center w-8 h-8 rounded-md text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-700"
          >
            <Icon name="close" size={16} />
          </button>
        </div>
        <div className="px-4 sm:px-5 py-3 max-h-[75vh] overflow-y-auto">
          {children}
        </div>
        {footer && (
          <div className="px-4 sm:px-5 py-3 border-t border-slate-200 dark:border-slate-700 bg-slate-50/60 dark:bg-slate-700/30">
            <div className="flex items-center justify-end gap-2">{footer}</div>
          </div>
        )}
      </div>
    </div>
  );
}


