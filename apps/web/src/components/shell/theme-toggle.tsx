/**
 * Dark/light theme toggle button for the top bar.
 *
 * Purpose:
 *   Renders a Sun/Moon icon button that toggles between dark and light themes
 *   using next-themes. Accessible with keyboard and screen readers.
 *
 * Related files:
 *   - src/components/shell/theme-provider.tsx (ThemeProvider context)
 *   - src/components/shell/top-bar.tsx (places this in the top bar)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

"use client";

import { Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { Button } from "@/components/ui/button";

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();

  function toggle() {
    setTheme(theme === "dark" ? "light" : "dark");
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      className="h-8 w-8 text-muted-foreground hover:text-foreground"
      onClick={toggle}
      aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
    >
      <Sun className="h-4 w-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
      <Moon className="absolute h-4 w-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
    </Button>
  );
}
