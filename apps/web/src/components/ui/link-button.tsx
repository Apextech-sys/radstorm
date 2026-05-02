/**
 * LinkButton — styled link that looks like a Button.
 *
 * Purpose:
 *   The installed shadcn Button uses @base-ui/react which doesn't support
 *   the asChild prop. This component provides a Link styled identically
 *   to a Button using the same buttonVariants CVA class generator.
 *
 * Related files:
 *   - src/components/ui/button.tsx (buttonVariants source)
 *   - Various page files that need button-styled navigation links
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import React from "react";
import Link from "next/link";
import { type VariantProps } from "class-variance-authority";
import { buttonVariants } from "./button";
import { cn } from "@/lib/utils";

interface LinkButtonProps
  extends React.AnchorHTMLAttributes<HTMLAnchorElement>,
    VariantProps<typeof buttonVariants> {
  href: string;
}

export function LinkButton({
  href,
  className,
  variant = "default",
  size = "default",
  children,
  ...props
}: LinkButtonProps) {
  return (
    <Link
      href={href}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    >
      {children}
    </Link>
  );
}
