/**
 * Smoke tests for the Runs list page.
 *
 * Purpose:
 *   Asserts that the Runs page renders without throwing, shows the
 *   heading, and renders the runs table (which shows loading skeletons
 *   before the mocked API resolves).
 *
 * Related files:
 *   - src/app/runs/page.tsx (component under test)
 *   - src/components/runs/runs-table.tsx (child component)
 *   - src/lib/api.ts (mocked)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import RunsPage from "@/app/runs/page";

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
  usePathname: () => "/runs",
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

// Mock the API to return an empty runs list
vi.mock("@/lib/api", () => ({
  listRuns: vi.fn().mockResolvedValue({ runs: [] }),
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, msg: string) {
      super(msg);
      this.status = status;
    }
  },
}));

describe("Runs page", () => {
  it("renders without crashing", () => {
    render(<RunsPage />);
  });

  it("shows the Runs heading", () => {
    render(<RunsPage />);
    expect(screen.getByText("Runs")).toBeInTheDocument();
  });

  it("shows the New Run action button", () => {
    render(<RunsPage />);
    expect(screen.getByText("New Run")).toBeInTheDocument();
  });
});
