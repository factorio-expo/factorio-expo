// The sites a profile links (Go `catalog.socialPlatforms`, which checks the
// names and builds the links): label and icon. Only a name is ever typed; the
// API refuses links.
import type { SocialPlatform } from "@lib/api";

export interface SocialPlatformInfo {
	platform: SocialPlatform;
	label: string;
	/** The Simple Icons file in public/third-party/simple-icons/. */
	icon: string;
	/** The brand colour from Simple Icons. */
	color: string;
	/** On a dark theme, for the brands whose colour is black. */
	darkColor?: string;
}

/** In the API's display order. */
export const SOCIAL_PLATFORMS: SocialPlatformInfo[] = [
	{ platform: "youtube", label: "YouTube", icon: "youtube", color: "#FF0000" },
	{ platform: "twitch", label: "Twitch", icon: "twitch", color: "#9146FF" },
	{ platform: "github", label: "GitHub", icon: "github", color: "#181717", darkColor: "#FFFFFF" },
	{ platform: "reddit", label: "Reddit", icon: "reddit", color: "#FF4500" },
	{ platform: "bluesky", label: "Bluesky", icon: "bluesky", color: "#0285FF" },
	{ platform: "steam", label: "Steam", icon: "steam", color: "#000000", darkColor: "#FFFFFF" },
	{ platform: "x", label: "X", icon: "x", color: "#000000", darkColor: "#FFFFFF" },
];

export function isSocialPlatform(value: string | undefined): value is SocialPlatform {
	return SOCIAL_PLATFORMS.some((info) => info.platform === value);
}

export function socialPlatform(platform: SocialPlatform): SocialPlatformInfo | undefined {
	return SOCIAL_PLATFORMS.find((info) => info.platform === platform);
}

export function socialIconUrl(info: SocialPlatformInfo): string {
	return `/third-party/simple-icons/${info.icon}.svg`;
}

/** The inline custom properties of a site's mark (read by `.brand` rules). */
export function socialBrandStyle(info: SocialPlatformInfo): string {
	const dark = info.darkColor ? `; --brand-dark: ${info.darkColor}` : "";
	return `--logo: url("${socialIconUrl(info)}"); --brand: ${info.color}${dark}`;
}
