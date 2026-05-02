/**
 * Smoke tests for the Settings page.
 *
 * Purpose:
 *   Asserts that the Settings page renders without throwing and shows
 *   the heading and Wave 2 coming placeholder.
 *
 * Related files:
 *   - src/app/settings/page.tsx (component under test)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import SettingsPage from "@/app/settings/page";

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
  usePathname: () => "/settings",
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

describe("Settings page", () => {
  it("renders without crashing", () => {
    render(<SettingsPage />);
  });

  it("shows the Settings heading", () => {
    render(<SettingsPage />);
    expect(screen.getByText("Settings")).toBeInTheDocument();
  });

  it("shows the Wave 2 placeholder", () => {
    render(<SettingsPage />);
    expect(screen.getByText(/Wave 2/i)).toBeInTheDocument();
  });
});
