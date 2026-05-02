/**
 * Smoke tests for the New Run page.
 *
 * Purpose:
 *   Asserts that the New Run page renders without throwing, shows
 *   the heading, and renders the scenario form (Start Run button).
 *
 * Related files:
 *   - src/app/runs/new/page.tsx (component under test)
 *   - src/components/runs/scenario-form.tsx (form child)
 *   - src/lib/api.ts (mocked)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import NewRunPage from "@/app/runs/new/page";

vi.mock("next/link", () => ({
  default: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
}));

vi.mock("next/navigation", () => ({
  usePathname: () => "/runs/new",
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

// Mock API calls made by the form on mount
vi.mock("@/lib/api", () => ({
  getScenarioTemplates: vi.fn().mockResolvedValue([]),
  createRun: vi.fn(),
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, msg: string) {
      super(msg);
      this.status = status;
    }
  },
}));

describe("New Run page", () => {
  it("renders without crashing", () => {
    render(<NewRunPage />);
  });

  it("shows the New Run heading", () => {
    render(<NewRunPage />);
    expect(screen.getByText("New Run")).toBeInTheDocument();
  });

  it("shows the Start Run button", () => {
    render(<NewRunPage />);
    // There are two "Start Run" buttons: header and footer submit
    const btns = screen.getAllByText(/Start Run/i);
    expect(btns.length).toBeGreaterThanOrEqual(1);
  });
});
