import React from "react";
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import TransactionStats from "./TransactionStats";

describe("TransactionStats", () => {
  it.each([
    [10000, 6000, "+40%"],
    [10000, 15000, "-50%"],
    [10000, 3333, "+67%"],
  ])("displays the signed rounded result for %s income and %s expenses", (totalIncome, totalExpenses, expected) => {
    render(<TransactionStats totalIncome={totalIncome} totalExpenses={totalExpenses} currencyCode="RUB" period="synthetic period" />);
    expect(screen.getByText(expected)).toBeInTheDocument();
  });

  it("does not calculate a percentage without income", () => {
    render(<TransactionStats totalIncome={0} totalExpenses={1000} currencyCode="RUB" />);
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });
});
