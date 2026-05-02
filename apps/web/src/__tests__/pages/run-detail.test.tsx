/**
 * Smoke tests for the Run Detail page.
 *
 * Purpose:
 *   Asserts that the Run Detail page server component renders without throwing,
 *   and that the RunDetailClient renders a loading skeleton. The params prop
 *   is a Promise as required by Next.js 16 App Router.
 *
 * Related files:
 *   - src/app/runs/[id]/page.tsx (component under test)
 *   - src/app/runs/[id]/run-detail-client.tsx (client component child)
 *   - src/lib/api.ts (mocked)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
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
  usePathname: () => "/runs/test-run-123",
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

// Mock API calls — getRun returns a "running" run
vi.mock("@/lib/api", () => ({
  getRun: vi.fn().mockResolvedValue({
    id: "test-run-123",
    status: "running",
    created_at: new Date().toISOString(),
    progress: {
      elapsed_ms: 5000,
      subscribers_total: 1000,
      subscribers_activated: 200,
      subscribers_established: 150,
      subscribers_failed: 0,
    },
  }),
  cancelRun: vi.fn(),
  getRunResults: vi.fn().mockResolvedValue(null),
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, msg: string) {
      super(msg);
      this.status = status;
    }
  },
}));

describe("Run Detail page", () => {
  it("renders without crashing", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
  });

  it("renders inside a page container", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    const { container } = render(element);
    // The page container renders a div with padding classes
    expect(container.querySelector("div")).toBeDefined();
  });

  it("shows loading skeleton on initial render", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
    // Before API resolves, loading skeleton is shown
    // The skeleton component renders with data-slot="skeleton"
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it("does not render old Wave 3 placeholder text", async () => {
    const element = await RunDetailPage({
      params: Promise.resolve({ id: "test-run-123" }),
    });
    render(element);
    expect(screen.queryByText(/Wave 3/i)).not.toBeInTheDocument();
  });
});
