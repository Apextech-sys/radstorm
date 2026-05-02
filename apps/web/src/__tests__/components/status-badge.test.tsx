/**
 * Unit tests for the StatusBadge component.
 *
 * Purpose:
 *   Verifies that each RunStatus value renders the correct label and that
 *   the running status includes an animated dot indicator.
 *
 * Related files:
 *   - src/components/runs/status-badge.tsx (component under test)
 *   - src/lib/types/api.ts (RunStatus enum)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { StatusBadge } from "@/components/runs/status-badge";
import type { RunStatus } from "@/lib/types/api";

const STATUSES: Array<[RunStatus, string]> = [
  ["queued", "Queued"],
  ["running", "Running"],
  ["succeeded", "Succeeded"],
  ["failed", "Failed"],
  ["cancelling", "Cancelling"],
  ["cancelled", "Cancelled"],
];

describe("StatusBadge", () => {
  it.each(STATUSES)("renders label for status=%s", (status, label) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("shows animated dot for running status", () => {
    const { container } = render(<StatusBadge status="running" />);
    const dot = container.querySelector(".animate-pulse");
    expect(dot).toBeTruthy();
  });

  it("does not show animated dot for succeeded status", () => {
    const { container } = render(<StatusBadge status="succeeded" />);
    const dot = container.querySelector(".animate-pulse");
    expect(dot).toBeNull();
  });
});
