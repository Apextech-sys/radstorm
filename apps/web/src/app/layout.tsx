/**
 * Root layout for the radstorm Next.js frontend.
 *
 * Purpose:
 *   Sets up global fonts (Geist Sans + Geist Mono), theme provider (dark-first),
 *   tooltip provider, and the full-height app shell. Every page in the app is
 *   rendered inside the AppShell providing consistent sidebar + top bar.
 *
 * Related files:
 *   - src/app/globals.css (design tokens and base styles)
 *   - src/components/layout/app-shell.tsx (sidebar + top bar layout)
 *   - src/components/shell/theme-provider.tsx (next-themes wrapper)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { ThemeProvider } from "@/components/shell/theme-provider";
import { AppShell } from "@/components/layout/app-shell";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
  display: "swap",
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
  display: "swap",
});

export const metadata: Metadata = {
  title: {
    template: "%s — radstorm",
    default: "radstorm — RADIUS Stress Tester",
  },
  description:
    "Stress-test ISP RADIUS servers at scale. Simulate up to 1M PPPoE/MAC subscribers with realistic authentication, accounting, and CoA/disconnect storms.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${geistSans.variable} ${geistMono.variable} h-full`}
    >
      <body className="h-full">
        <ThemeProvider
          attribute="class"
          defaultTheme="dark"
          enableSystem
          disableTransitionOnChange
        >
          <TooltipProvider>
            <AppShell>{children}</AppShell>
            <Toaster />
          </TooltipProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
