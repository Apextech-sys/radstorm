/**
 * Smoke tests for the Runs list page.
 *
 * Purpose:
 *   Asserts that the Runs page renders without throwing and contains
 *   the expected heading and "Coming in Wave 2" placeholder.
 *
 * Related files:
 *   - src/app/runs/page.tsx (component under test)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
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

describe("Runs page", () => {
  it("renders without crashing", () => {
    render(<RunsPage />);
  });

  it("shows the Runs heading", () => {
    render(<RunsPage />);
    expect(screen.getByText("Runs")).toBeInTheDocument();
  });

  it("shows the Wave 2 placeholder", () => {
    render(<RunsPage />);
    expect(screen.getByText(/Wave 2/i)).toBeInTheDocument();
  });
});
