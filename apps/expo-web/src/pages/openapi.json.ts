// The read API's OpenAPI document for tools and generators: the API's own
// document without its internal operations (scripts/api-docs.ts);
// /developers/api renders the same file.
import type { APIRoute } from "astro";
import spec from "../generated/read-api.json?raw";

export const prerender = true;

export const GET: APIRoute = () =>
	new Response(spec, {
		headers: {
			"Content-Type": "application/json; charset=utf-8",
			"Access-Control-Allow-Origin": "*",
		},
	});
