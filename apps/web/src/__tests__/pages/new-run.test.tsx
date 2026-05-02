/**
 * Smoke tests for the New Run page.
 *
 * Purpose:
 *   Asserts that the New Run page renders without throwing and shows
 *   the heading and Wave 2 coming placeholder.
 *
 * Related files:
 *   - src/app/runs/new/page.tsx (component under test)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
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

describe("New Run page", () => {
  it("renders without crashing", () => {
    render(<NewRunPage />);
  });

  it("shows the New Run heading", () => {
    render(<NewRunPage />);
    expect(screen.getByText("New Run")).toBeInTheDocument();
  });

  it("shows the Wave 2 placeholder", () => {
    render(<NewRunPage />);
    expect(screen.getByText(/Wave 2/i)).toBeInTheDocument();
  });
});
