import { memo, ReactNode } from "react";
import { cn } from "@/lib/utils";

interface Tab {
  id: string;
  label: string;
  content: ReactNode;
}

interface TabsProps {
  tabs: Tab[];
  activeTab: string;
  onTabChange: (tabId: string) => void;
  className?: string;
}

const Tabs = memo(function Tabs({
  tabs,
  activeTab,
  onTabChange,
  className,
}: TabsProps) {
  const onKeyDown = (e: React.KeyboardEvent, index: number) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const dir = e.key === "ArrowRight" ? 1 : -1;
    const next = (index + dir + tabs.length) % tabs.length;
    onTabChange(tabs[next].id);
  };

  return (
    <div className={cn("w-full", className)}>
      {/* Tab Headers */}
      <div className="border-b border-border">
        <nav role="tablist" className="-mb-px flex space-x-8">
          {tabs.map((tab, index) => {
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                role="tab"
                aria-selected={active}
                tabIndex={active ? 0 : -1}
                onKeyDown={(e) => onKeyDown(e, index)}
                onClick={() => onTabChange(tab.id)}
                className={cn(
                  "whitespace-nowrap py-3 px-1 border-b-2 font-medium text-sm transition-colors duration-200 flex items-center justify-center",
                  active
                    ? "border-primary text-primary"
                    : "border-transparent text-muted-foreground hover:text-foreground hover:border-border"
                )}
              >
                {tab.label}
              </button>
            );
          })}
        </nav>
      </div>

      {/* Tab Content */}
      <div role="tabpanel" className="mt-6">
        {tabs.find((tab) => tab.id === activeTab)?.content}
      </div>
    </div>
  );
});

export default Tabs;
