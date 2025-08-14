"use client";

import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components";
import { CategoryKind } from "@/proto/budget/v1/common_pb";

interface Props {
  missingNames: string[];
  allCategories: any[];
  suggestedKinds?: Record<string, CategoryKind>;
  onQuickCreate: (name: string, kind: CategoryKind) => Promise<any>;
  onComplete: (mapping: Record<string, string>) => void;
}

export default function CategoryMapping({ missingNames, allCategories, suggestedKinds, onQuickCreate, onComplete }: Props) {
  const [map, setMap] = useState<Record<string, string>>({});
  const [creating, setCreating] = useState<string | null>(null);
  const [localCategories, setLocalCategories] = useState<any[]>(allCategories || []);

  useEffect(() => {
    setLocalCategories(allCategories || []);
  }, [allCategories]);

  const categoriesByKind = useMemo(() => {
    return {
      income: (localCategories || []).filter((c: any) => c.kind === CategoryKind.INCOME),
      expense: (localCategories || []).filter((c: any) => c.kind === CategoryKind.EXPENSE),
    };
  }, [localCategories]);

  const guessKind = (name: string): CategoryKind => {
    if (suggestedKinds && suggestedKinds[name] !== undefined) return suggestedKinds[name];
    return CategoryKind.EXPENSE;
  };

  return (
    <div className="space-y-3">
      {missingNames.map((name) => {
        const kind = guessKind(name);
        const list = kind === CategoryKind.INCOME ? categoriesByKind.income : categoriesByKind.expense;
        return (
          <div key={name} className="flex items-center gap-2">
            <div className="w-56 truncate" title={name}>{name}</div>
            <select
              className="min-w-[240px] border rounded px-2 py-1 bg-white dark:bg-slate-700"
              value={map[name.toLowerCase()] || ""}
              onChange={(e) => setMap((m) => ({ ...m, [name.toLowerCase()]: e.target.value }))}
            >
              <option value="">— Выберите категорию —</option>
              {list.map((c: any) => (
                <option key={c.id} value={c.id}>{c.translations?.[0]?.name || c.code}</option>
              ))}
            </select>
            <Button
              variant="secondary"
              size="sm"
              loading={creating === name}
              disabled={!!map[name.toLowerCase()]}
              onClick={async () => {
                setCreating(name);
                try {
                  // If category with this code/name already exists locally, just select it
                  const exists = (localCategories || []).find((c: any) =>
                    (c.code || "").toLowerCase() === name.toLowerCase() ||
                    (c.translations || []).some((t: any) => (t.name || "").toLowerCase() === name.toLowerCase())
                  );
                  if (exists) {
                    setMap((m) => ({ ...m, [name.toLowerCase()]: exists.id }));
                  } else {
                    const created = await onQuickCreate(name, kind);
                    if (created) {
                      setLocalCategories((prev) => [...prev, created]);
                      setMap((m) => ({ ...m, [name.toLowerCase()]: created.id }));
                    }
                  }
                } finally {
                  setCreating(null);
                }
              }}
            >
              Быстро создать
            </Button>
          </div>
        );
      })}
      <div className="pt-2">
        <Button variant="primary" onClick={() => onComplete(map)}>
          Далее
        </Button>
      </div>
    </div>
  );
}


