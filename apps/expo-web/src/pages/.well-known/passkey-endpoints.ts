// Where password managers send people to add or manage a passkey for this
// site (W3C "A Well-Known URL for Relying Party Passkey Endpoints"). Both are
// the account page's sign-in methods; it sends signed-out visitors to /login.
// Rendered on demand: a prerendered file without an extension would be
// served as application/octet-stream.
import type { APIRoute } from "astro";

export const GET: APIRoute = ({ site, url }) => {
	const signInMethods = new URL("/account#sign-in-methods", site ?? url).toString();
	return new Response(JSON.stringify({ enroll: signInMethods, manage: signInMethods }), {
		headers: { "Content-Type": "application/json" },
	});
};
