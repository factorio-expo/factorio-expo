// The operator's details for this build (lib/legal.ts parseLegal). Kept out
// of legal.ts so its unit tests need no Astro runtime.
import { PUBLIC_LEGAL } from "astro:env/client";
import { parseLegal } from "@lib/legal";

export const LEGAL = parseLegal(PUBLIC_LEGAL);
