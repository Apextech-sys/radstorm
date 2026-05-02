/**
 * Unit tests for format utility functions.
 *
 * Purpose:
 *   Verifies number formatting, duration formatting, and subscriber
 *   preset labels behave correctly for boundary values.
 *
 * Related files:
 *   - src/lib/format.ts (functions under test)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import { describe, it, expect } from "vitest";
import {
  fmtCompact,
  fmtInt,
  fmtPct,
  fmtDurationMs,
  fmtLatency,
  subscriberLabel,
  SUBSCRIBER_PRESETS,
} from "@/lib/format";

describe("fmtCompact", () => {
  it("formats small numbers without suffix", () => {
    expect(fmtCompact(999)).toBe("999");
  });

  it("formats thousands with K suffix", () => {
    expect(fmtCompact(1000)).toBe("1K");
  });

  it("formats millions with M suffix", () => {
    expect(fmtCompact(1_000_000)).toBe("1M");
  });
});

describe("fmtInt", () => {
  it("formats integers with thousand separators", () => {
    expect(fmtInt(12345)).toBe("12,345");
  });

  it("rounds floating point numbers", () => {
    expect(fmtInt(12.7)).toBe("13");
  });
});

describe("fmtPct", () => {
  it("returns 0% when total is 0", () => {
    expect(fmtPct(0, 0)).toBe("0%");
  });

  it("calculates percentage correctly", () => {
    expect(fmtPct(998, 1000)).toBe("99.8%");
  });

  it("handles 100%", () => {
    expect(fmtPct(1000, 1000)).toBe("100.0%");
  });
});

describe("fmtDurationMs", () => {
  it("formats sub-second durations in ms", () => {
    expect(fmtDurationMs(500)).toBe("500ms");
  });

  it("formats second-range durations", () => {
    expect(fmtDurationMs(5_000)).toBe("5.0s");
  });

  it("formats minute-range durations", () => {
    expect(fmtDurationMs(65_000)).toBe("1m 5s");
  });
});

describe("fmtLatency", () => {
  it("shows ms for sub-second values", () => {
    expect(fmtLatency(250)).toBe("250ms");
  });

  it("shows seconds for >= 1000ms", () => {
    expect(fmtLatency(1500)).toBe("1.50s");
  });
});

describe("subscriberLabel", () => {
  it("shows plain number for <1000", () => {
    expect(subscriberLabel(100)).toBe("100");
  });

  it("shows K suffix for thousands", () => {
    expect(subscriberLabel(1000)).toBe("1K");
  });

  it("shows M suffix for millions", () => {
    expect(subscriberLabel(1_000_000)).toBe("1M");
  });
});

describe("SUBSCRIBER_PRESETS", () => {
  it("has 5 presets", () => {
    expect(SUBSCRIBER_PRESETS).toHaveLength(5);
  });

  it("starts at 100 and ends at 1M", () => {
    expect(SUBSCRIBER_PRESETS[0].value).toBe(100);
    expect(SUBSCRIBER_PRESETS[SUBSCRIBER_PRESETS.length - 1].value).toBe(1_000_000);
  });
});
