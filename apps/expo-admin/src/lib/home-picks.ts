// The Home picks page's form: what staff paste to pick a blueprint.

const SHORT_ID = /^[0-9A-Za-z]{12}$/;

/**
 * The blueprint id in what was pasted: a blueprint page link of the site
 * (`/e/{entry}/b/{blueprint}`, any host, query or fragment) or a bare id.
 * Undefined when it names no blueprint (an entry link has no blueprint id).
 */
export function pickedBlueprintId(input: string): string | undefined {
	const text = input.trim();
	if (SHORT_ID.test(text)) {
		return text;
	}
	let path = text;
	try {
		path = new URL(text, "https://site.invalid").pathname;
	} catch {
		return undefined;
	}
	const match = /^\/e\/[0-9A-Za-z]{12}\/b\/([0-9A-Za-z]{12})\/?$/.exec(path);
	return match?.[1];
}
