// Pure helpers for the console's form posts and their display.

/** A trimmed text field; "" when missing. */
export function formText(form: FormData, name: string): string {
	const value = form.get(name);
	return typeof value === "string" ? value.trim() : "";
}

/** A whole number field within [min, max]; undefined when missing or out of range. */
export function formInt(
	form: FormData,
	name: string,
	min: number,
	max: number,
): number | undefined {
	const raw = formText(form, name);
	if (!/^\d+$/.test(raw)) return undefined;
	const value = Number(raw);
	return value >= min && value <= max ? value : undefined;
}

const DAY_MS = 24 * 60 * 60 * 1000;

/** The moment `days` days from now, as the API's RFC 3339 time. */
export function daysFromNow(days: number, now = Date.now()): string {
	return new Date(now + days * DAY_MS).toISOString();
}

const GIB = 1024 ** 3;

/** A size field in GiB (decimals allowed) as bytes; undefined when not a size. */
export function formGiB(form: FormData, name: string): number | undefined {
	const raw = formText(form, name);
	if (!/^\d+(\.\d+)?$/.test(raw)) return undefined;
	return Math.round(Number(raw) * GIB);
}

/**
 * The page to go back to after a form post: the same path and query, with
 * the outcome as ?notice= or ?error= in place of an earlier one.
 */
export function outcomeUrl(url: URL, kind: "notice" | "error", message: string): string {
	const next = new URL(url);
	next.searchParams.delete("notice");
	next.searchParams.delete("error");
	next.searchParams.set(kind, message);
	return next.pathname + next.search;
}
