/**
 * Smoke tests for the Run Detail page.
 *
 * Purpose:
 *   Asserts that the Run Detail page renders without throwing.
 *   Note: params is a Promise in Next.js 16 App Router, so we pass
 *   a resolved Promise in the test.
 *
 * Related files:
 *   - src/app/runs/[id]/page.tsx (component under test)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import RunDetailPage from "@/app/runs/[id]/page";

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
  usePathname: () => "/runs/test-run-id",
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

describe("Run Detail page", () => {
  it("renders without crashing", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
  });

  it("shows the Run Detail heading", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
    expect(screen.getByText("Run Detail")).toBeInTheDocument();
  });

  it("shows the run ID in description", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
    expect(screen.getByText("Run test-run-123")).toBeInTheDocument();
  });

  it("shows the Wave 3 placeholder", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
    expect(screen.getByText(/Wave 3/i)).toBeInTheDocument();
  });
});
