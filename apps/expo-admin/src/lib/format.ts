// Display formatting shared by the console pages (times: @factorio-expo/ui/utils/dates.ts).

const GIB = 1024 ** 3;

/** Bytes for people: 1.5 GiB, 340 MiB, 12 KiB, 7 B. */
export function formatBytes(bytes: number): string {
	const units = ["B", "KiB", "MiB", "GiB", "TiB"];
	let value = bytes;
	let unit = 0;
	while (Math.abs(value) >= 1024 && unit < units.length - 1) {
		value /= 1024;
		unit++;
	}
	const digits = unit === 0 || value >= 100 ? 0 : 1;
	return `${value.toFixed(digits)} ${units[unit]}`;
}

/** Bytes as a GiB form value (two decimals at most). */
export function toGiB(bytes: number): string {
	return String(Math.round((bytes / GIB) * 100) / 100);
}

const providerLabels: Record<string, string> = {
	github: "GitHub",
	discord: "Discord",
	steam: "Steam",
	google: "Google",
};

/** An account's sign-in methods: "GitHub, Steam, 2 passkeys". */
export function signInMethods(user: { providers: string[]; passkeys: number }): string {
	const parts = user.providers.map((provider) => providerLabels[provider] ?? provider);
	if (user.passkeys > 0)
		parts.push(user.passkeys === 1 ? "1 passkey" : `${user.passkeys} passkeys`);
	return parts.length > 0 ? parts.join(", ") : "none";
}
