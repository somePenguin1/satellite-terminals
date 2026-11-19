import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HealthBar } from "./HealthBar";

const counts = { healthy: 4500, degraded: 120, down: 20, offline: 160, inactive: 200 };

describe("HealthBar", () => {
  it("shows counts with their share of the fleet", () => {
    render(<HealthBar counts={counts} />);
    expect(screen.getByText("4,500")).toBeInTheDocument();
    expect(screen.getByText("90.0%")).toBeInTheDocument();
  });

  it("toggles a health filter", async () => {
    const onSelect = vi.fn();
    const { rerender } = render(<HealthBar counts={counts} onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button", { name: /degraded/i }));
    expect(onSelect).toHaveBeenLastCalledWith("degraded");

    rerender(<HealthBar counts={counts} selected="degraded" onSelect={onSelect} />);
    expect(screen.getByRole("button", { name: /degraded/i })).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(screen.getByRole("button", { name: /degraded/i }));
    expect(onSelect).toHaveBeenLastCalledWith(null);
  });
});
