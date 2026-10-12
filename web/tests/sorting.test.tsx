import React, { useState } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { SortableHeader, type SortDirection } from "../components/SortableHeader";

describe("SortableHeader", () => {
  it.each([
    ["amount_numeric", "asc"],
    ["occurred_at", "desc"],
    ["comment", "asc"],
  ] as const)("cycles %s through its default, opposite and cleared sort", (field, defaultDirection) => {
    const onSort = vi.fn();
    function Table() {
      const [sort, setSort] = useState("");
      const update = (key: string, direction: SortDirection) => {
        onSort(key, direction);
        setSort(direction ? `${key} ${direction}` : "");
      };
      return <table><thead><tr><SortableHeader field={field} currentSort={sort} defaultDirection={defaultDirection} onSort={update}>Sort</SortableHeader></tr></thead></table>;
    }
    render(<Table />);
    const header = screen.getByRole("columnheader", { name: "Sort" });
    fireEvent.click(header);
    fireEvent.click(header);
    fireEvent.click(header);
    expect(onSort.mock.calls).toEqual([[field, defaultDirection], [field, defaultDirection === "asc" ? "desc" : "asc"], [field, null]]);
  });
});
