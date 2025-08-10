"use client";

import Icon from "./Icon";
import type { IconName } from "./Icon";

interface EmptyStateProps {
  icon: IconName;
  title: string;
  description: string;
  action?: {
    label: string;
    href: string;
  };
}

export default function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="text-center py-12">
      <div className="inline-flex items-center justify-center w-16 h-16 bg-muted rounded-full mb-6">
        <Icon name={icon} size={32} className="text-muted-foreground" />
      </div>
      <h3 className="text-lg font-medium text-foreground mb-2">{title}</h3>
      <p className="text-muted-foreground mb-6 max-w-md mx-auto">{description}</p>
      {action && (
        <a
          href={action.href}
          className="btn btn-primary inline-flex items-center"
        >
          <Icon name="plus" size={16} className="mr-2" />
          {action.label}
        </a>
      )}
    </div>
  );
}
