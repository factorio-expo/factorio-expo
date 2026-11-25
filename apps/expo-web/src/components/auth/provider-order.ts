// Display order for provider buttons: as the API lists them, except Steam
// last, since its required official image stands out from the others.
import type { SignInProvider } from "@lib/api";

export function inDisplayOrder<T extends SignInProvider>(providers: T[]): T[] {
	const last = (p: T) => (p.id === "steam" ? 1 : 0);
	return providers.toSorted((a, b) => last(a) - last(b));
}
