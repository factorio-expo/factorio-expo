// Builds the read API's published document and docs page from the API's
// OpenAPI document (apps/expo-api/openapi/public.json) into src/generated/:
// read-api.json (served at /openapi.json) without the x-internal operations
// (redocly.yaml), and read-api.html (served at /developers/api), Redoc's
// static page with its scripts inlined. Run before `astro dev` and
// `astro build`; the outputs are not checked in.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const web = fileURLToPath(new URL("..", import.meta.url));
const source = fileURLToPath(new URL("../../expo-api/openapi/public.json", import.meta.url));
const outDir = fileURLToPath(new URL("../src/generated/", import.meta.url));
const json = `${outDir}read-api.json`;
const html = `${outDir}read-api.html`;

// The CLI's own script, run with this Node: no shell, so arguments stay whole.
const cli = createRequire(import.meta.url).resolve("@redocly/cli/bin/cli.js");

/**
 * Drops the "now renders docs with Redoc 3" box `build-docs` prints on every
 * run: informational only, with no option to silence it.
 */
function withoutRedoc3Banner(output: string): string {
	let inBox = false;
	return output
		.split("\n")
		.filter((line) => {
			if (line.includes("╔")) inBox = true;
			const drop = inBox;
			if (line.includes("╝")) inBox = false;
			return !drop;
		})
		.join("\n");
}

/** Runs the Redocly CLI without its update check (telemetry is off in redocly.yaml). */
function redocly(...args: string[]) {
	const { stdout, stderr, status } = spawnSync(process.execPath, [cli, ...args], {
		cwd: web,
		stdio: ["inherit", "pipe", "pipe"],
		encoding: "utf8",
		env: { ...process.env, REDOCLY_SUPPRESS_UPDATE_NOTICE: "true" },
	});
	process.stdout.write(withoutRedoc3Banner(stdout));
	process.stderr.write(withoutRedoc3Banner(stderr));
	if (status !== 0) process.exit(status ?? 1);
}

/**
 * The page's own CSP: its inline scripts by hash; Redoc injects its styles
 * at runtime. Prerendered pages are static files, so the CSP travels in the
 * page itself.
 */
function withCsp(page: string): string {
	const hashes = [...page.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)].map(
		([, body]) =>
			`'sha256-${createHash("sha256")
				.update(body ?? "")
				.digest("base64")}'`,
	);
	const policy = [
		"default-src 'self'",
		`script-src ${hashes.join(" ")}`,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	].join("; ");
	return page.replace(
		/<head>/,
		`<head>\n<meta http-equiv="Content-Security-Policy" content="${policy}">`,
	);
}

const DESCRIPTION =
	"The public, read-only Factorio Expo API: search, entries, blueprints, raw exchange strings, preview images.";

/** Escapes a value for an HTML attribute. */
function attr(value: string): string {
	return value.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;");
}

/**
 * The site's head tags Redoc's template leaves out (its metaDescription option
 * is ignored by Redoc 3): description, canonical, Open Graph and the favicon,
 * plus a link back to the site, which the docs page otherwise lacks.
 */
function withSiteChrome(page: string): string {
	const site = (process.env.PUBLIC_SITE_URL || "http://localhost:3000").replace(/\/+$/, "");
	const url = `${site}/developers/api`;
	const head = [
		`<meta name="description" content="${attr(DESCRIPTION)}">`,
		`<link rel="canonical" href="${attr(url)}">`,
		`<link rel="icon" type="image/png" href="/favicon.png">`,
		`<meta property="og:site_name" content="Factorio Expo">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:title" content="Read API · Factorio Expo">`,
		`<meta property="og:description" content="${attr(DESCRIPTION)}">`,
		`<meta property="og:url" content="${attr(url)}">`,
		`<meta property="og:image" content="${attr(`${site}/favicon.png`)}">`,
		`<meta name="twitter:card" content="summary">`,
		`<style>.site-back{display:block;padding:.6rem 1rem;border-bottom:1px solid #e5e7eb;background:#fff;color:#8a5300;font:600 .95rem/1.4 system-ui,sans-serif;text-decoration:none}.site-back:hover{text-decoration:underline}</style>`,
	].join("\n");
	return page
		.replace(/<\/title>/, `</title>\n${head}`)
		.replace(/<body>/, `<body>\n<a class="site-back" href="/">← Factorio Expo</a>`);
}

mkdirSync(outDir, { recursive: true });
redocly("bundle", source, "--config", "redocly.yaml", "-o", json);
redocly("build-docs", json, "--inlineBundle", "--title", "Read API · Factorio Expo", "-o", html);
writeFileSync(html, withCsp(withSiteChrome(readFileSync(html, "utf8"))));
