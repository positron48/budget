import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import CombinedChart from "@/components/CombinedChart";
import InteractiveChart from "@/components/InteractiveChart";
import { renderWithIntl } from "./utils";
import messages from "@/i18n/en.json";

const intl = { messages };

describe("report charts", () => {
  it("renders negative net income below the zero line", () => {
    renderWithIntl(
      <CombinedChart
        title="Summary"
        expenses={[{ id: "rent", name: "Rent", color: "#ef4444", values: [200], total: 200 }]}
        incomes={[{ id: "salary", name: "Salary", color: "#22c55e", values: [100], total: 100 }]}
        months={["Jan 2026"]}
        currencyCode="USD"
      />,
      intl
    );

    const zeroLine = screen.getAllByTestId("combined-zero-line")[0];
    const netPoint = screen.getByTestId("net-0");

    expect(Number(netPoint.getAttribute("cy"))).toBeGreaterThan(Number(zeroLine.getAttribute("y1")));
    expect(screen.getByTestId("net-line").getAttribute("points")).not.toContain("NaN");
  });

  it("shows a combined chart tooltip on point hover", () => {
    renderWithIntl(
      <CombinedChart
        title="Summary"
        expenses={[{ id: "rent", name: "Rent", color: "#ef4444", values: [50], total: 50 }]}
        incomes={[{ id: "salary", name: "Salary", color: "#22c55e", values: [150], total: 150 }]}
        months={["Jan 2026"]}
        currencyCode="USD"
      />,
      intl
    );

    fireEvent.mouseEnter(screen.getByTestId("net-0"), { clientX: 100, clientY: 100 });

    expect(screen.getByRole("tooltip")).toHaveTextContent("Jan 2026");
    expect(screen.getByRole("tooltip")).toHaveTextContent("100 USD");
  });

  it("shows an interactive chart tooltip on bar hover", () => {
    renderWithIntl(
      <InteractiveChart
        title="Expenses"
        categories={[{ id: "food", name: "Food", color: "#ef4444", values: [75], total: 75 }]}
        months={["Jan 2026"]}
        currencyCode="USD"
      />,
      intl
    );

    fireEvent.mouseEnter(screen.getByTestId("bar-food-0"), { clientX: 100, clientY: 100 });

    expect(screen.getByRole("tooltip")).toHaveTextContent("Jan 2026");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Food: 75 USD");
  });

  it("renders one-month line coordinates without NaN", () => {
    renderWithIntl(
      <InteractiveChart
        title="Income"
        categories={[{ id: "salary", name: "Salary", color: "#22c55e", values: [100], total: 100 }]}
        months={["Jan 2026"]}
        currencyCode="USD"
      />,
      intl
    );

    fireEvent.click(screen.getAllByRole("button")[0]);

    expect(screen.getByTestId("line-salary-0").getAttribute("cx")).not.toBe("NaN");
  });
});
