// Who runs the site and how to reach them, for the terms of use, privacy
// policy, about page and /.well-known/security.txt. The values come from the
// PUBLIC_LEGAL build variable (lib/legal-env.ts), so they are not in the
// repository; a missing field shows as an `[OWNER: ...]` placeholder.

/** The operator's details, as PUBLIC_LEGAL holds them. */
export interface Legal {
	/** The operator: the controller under the GDPR. */
	name: string;
	/** The one address for everything: privacy, abuse, takedowns, security, authorities. */
	email: string;
	/** The server's hosting company and where the server is (a processor under Art. 28 GDPR). */
	hostingProvider: string;
	/** Who hosts the contact address's mailbox and where (a processor under Art. 28 GDPR). */
	emailProvider: string;
}

/** What a page shows for a field PUBLIC_LEGAL leaves out. */
export const LEGAL_PLACEHOLDERS: Legal = {
	name: "[OWNER: full name]",
	email: "[OWNER: contact email address]",
	hostingProvider: "[OWNER: hosting company and the server's country]",
	emailProvider: "[OWNER: email provider and its country]",
};

/** Languages the contact address is answered in. */
export const CONTACT_LANGUAGES = ["English"] as const;

const isLegalKey = (key: string): key is keyof Legal => Object.hasOwn(LEGAL_PLACEHOLDERS, key);

/**
 * Reads PUBLIC_LEGAL: a JSON object with any of the Legal fields. Unset, an
 * empty field or a missing one falls back to its placeholder; anything else
 * malformed (not JSON, an unknown key, a non-string) throws, so a typo fails
 * the build instead of publishing a placeholder.
 */
export function parseLegal(raw: string | undefined): Legal {
	if (!raw?.trim()) return { ...LEGAL_PLACEHOLDERS };
	let parsed: unknown;
	try {
		parsed = JSON.parse(raw);
	} catch (err) {
		throw new Error(`PUBLIC_LEGAL is not valid JSON: ${String(err)}`, { cause: err });
	}
	if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
		throw new Error("PUBLIC_LEGAL must be a JSON object");
	}
	const legal = { ...LEGAL_PLACEHOLDERS };
	for (const [key, value] of Object.entries(parsed)) {
		if (!isLegalKey(key)) {
			const known = Object.keys(LEGAL_PLACEHOLDERS).join(", ");
			throw new Error(`PUBLIC_LEGAL has an unknown key "${key}" (known: ${known})`);
		}
		if (typeof value !== "string") throw new Error(`PUBLIC_LEGAL.${key} must be a string`);
		if (value.trim()) legal[key] = value.trim();
	}
	return legal;
}

/** How long a published security.txt stays valid (RFC 9116 asks for under a year). */
const SECURITY_TXT_LIFETIME_DAYS = 360;

/**
 * /.well-known/security.txt (RFC 9116). Its Expires is counted from `now`,
 * the build time of the prerendered file, so every deploy renews it.
 */
export function securityTxt(site: URL, now: Date, email: string): string {
	const expires = new Date(now.getTime() + SECURITY_TXT_LIFETIME_DAYS * 24 * 60 * 60 * 1000);
	return [
		`Contact: mailto:${email}`,
		`Expires: ${expires.toISOString().replace(/\.\d{3}Z$/, "Z")}`,
		"Preferred-Languages: en",
		`Canonical: ${new URL("/.well-known/security.txt", site).toString()}`,
		"",
	].join("\n");
}
