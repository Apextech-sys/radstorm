/**
 * Theme provider wrapping next-themes for dark/light mode switching.
 *
 * Purpose:
 *   Wraps the app with next-themes ThemeProvider, enabling dark-first theme
 *   toggle throughout the application. Defaults to dark mode.
 *
 * Related files:
 *   - src/app/layout.tsx (wraps the entire app)
 *   - src/components/shell/top-bar.tsx (theme toggle button)
 *   - src/app/globals.css (dark/light CSS variables)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

"use client";

import * as React from "react";
import { ThemeProvider as NextThemesProvider } from "next-themes";

type ThemeProviderProps = React.ComponentProps<typeof NextThemesProvider>;

export function ThemeProvider({ children, ...props }: ThemeProviderProps) {
  return <NextThemesProvider {...props}>{children}</NextThemesProvider>;
}
