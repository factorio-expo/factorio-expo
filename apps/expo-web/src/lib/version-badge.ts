import type { Compat21 } from "@lib/api";

export interface VersionBadge {
	/** "2.1" (made in 2.1), "2.1 compatible" or "2.1 incompatible". */
	text: string;
	/** Drives the badge colour. */
	tone: "native" | "compatible" | "incompatible";
	/** The badge's tooltip. */
	title: string;
}

/**
 * The Factorio 2.1 badge on a card's preview: made in 2.1, or made earlier and
 * judged by the API (`compat_21`). Nothing when the version is unknown.
 */
export function versionBadge(
	gameVersion: string | undefined,
	compat: Compat21 | undefined,
): VersionBadge | undefined {
	const [major, minor] = (gameVersion ?? "").split(".").map(Number);
	if (major === undefined || minor === undefined || Number.isNaN(major) || Number.isNaN(minor)) {
		return undefined;
	}
	if (major > 2 || (major === 2 && minor >= 1)) {
		return {
			text: "2.1",
			tone: "native",
			title: "Made in Factorio 2.1",
		} satisfies VersionBadge;
	}
	if (compat === "compatible" || compat === "incompatible") {
		return {
			text: `2.1 ${compat}`,
			tone: compat,
			title: `Made before 2.1: ${compat} with Factorio 2.1`,
		} satisfies VersionBadge;
	}
	return undefined;
}
