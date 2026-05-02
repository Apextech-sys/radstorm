/**
 * Smoke tests for the Dashboard page.
 *
 * Purpose:
 *   Asserts that the Dashboard page renders without throwing and contains
 *   expected key UI elements (heading, New Run link, capability labels).
 *
 * Related files:
 *   - src/app/page.tsx (component under test)
 *   - src/components/layout/page-container.tsx (PageHeader)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import DashboardPage from "@/app/page";

// Mock next/link — renders a plain <a> in tests
vi.mock("next/link", () => ({
  default: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
}));

// Mock next/navigation (used by sidebar)
vi.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({ push: vi.fn() }),
}));

// Mock next-themes (used by ThemeToggle)
vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

describe("Dashboard page", () => {
  it("renders without crashing", () => {
    render(<DashboardPage />);
  });

  it("shows the Dashboard heading", () => {
    render(<DashboardPage />);
    expect(screen.getByText("Dashboard")).toBeInTheDocument();
  });

  it("shows the New Run link", () => {
    render(<DashboardPage />);
    const newRunLinks = screen.getAllByRole("link", { name: /new run/i });
    expect(newRunLinks.length).toBeGreaterThan(0);
  });

  it("shows the empty state headline", () => {
    render(<DashboardPage />);
    expect(screen.getByText(/no runs yet/i)).toBeInTheDocument();
  });

  it("shows capability labels", () => {
    render(<DashboardPage />);
    expect(screen.getByText("Up to 1M subscribers")).toBeInTheDocument();
    expect(screen.getByText("Access + Accounting + CoA")).toBeInTheDocument();
  });
});
