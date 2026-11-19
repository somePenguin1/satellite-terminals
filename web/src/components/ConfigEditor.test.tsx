import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { DeviceConfig } from "../api/types";
import { ConfigEditor } from "./ConfigEditor";

const config: DeviceConfig = {
  terminalId: "SAT-00001",
  revision: 3,
  reporting: { intervalSeconds: 5, level: "basic" },
  radio: { band: "Ku", txPowerDbm: 27 },
  network: { qosProfile: "standard" },
  firmware: { channel: "stable", maintenanceWindow: { start: "02:00", end: "04:00" } },
  shaping: { downlinkMbps: 100, uplinkMbps: 10 },
  metadata: { updatedAt: new Date().toISOString(), updatedBy: "seed" },
};

describe("ConfigEditor", () => {
  it("only enables saving once something changed, and sends the v2 shape", async () => {
    const onSave = vi.fn();
    render(<ConfigEditor config={config} saving={false} error={null} onSave={onSave} onReload={() => {}} />);
    const save = screen.getByRole("button", { name: "Save configuration" });
    expect(save).toBeDisabled();

    const interval = screen.getByLabelText(/report every/i);
    await userEvent.clear(interval);
    await userEvent.type(interval, "2");
    await userEvent.selectOptions(screen.getByLabelText(/traffic priority/i), "priority");
    await userEvent.click(save);

    expect(onSave).toHaveBeenCalledWith({
      reporting: { intervalSeconds: 2, level: "basic" },
      radio: { band: "Ku", txPowerDbm: 27 },
      network: { qosProfile: "priority" },
      firmware: { channel: "stable", maintenanceWindow: { start: "02:00", end: "04:00" } },
    });
  });

  it("explains invalid input and blocks the save", async () => {
    const onSave = vi.fn();
    render(<ConfigEditor config={config} saving={false} error={null} onSave={onSave} onReload={() => {}} />);
    const power = screen.getByLabelText(/transmit power/i);
    await userEvent.clear(power);
    await userEvent.type(power, "50");
    expect(screen.getByText("Transmit power must be between 10 and 33 dBm.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Save configuration" }));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("offers a reload when someone else saved first", async () => {
    const onReload = vi.fn();
    const conflict = new ApiError(412, { title: "Precondition Failed" });
    render(<ConfigEditor config={config} saving={false} error={conflict} onSave={() => {}} onReload={onReload} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Someone else changed this configuration");
    await userEvent.click(screen.getByRole("button", { name: "Reload configuration" }));
    expect(onReload).toHaveBeenCalled();
  });
});
