// Where to report a vulnerability (RFC 9116); the contact is PUBLIC_LEGAL's.
import type { APIRoute } from "astro";
import { securityTxt } from "@lib/legal";
import { LEGAL } from "@lib/legal-env";

export const prerender = true;

export const GET: APIRoute = ({ site, url }) =>
	new Response(securityTxt(site ?? url, new Date(), LEGAL.email), {
		headers: { "Content-Type": "text/plain; charset=utf-8" },
	});
