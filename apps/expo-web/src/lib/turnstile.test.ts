import { describe, expect, it } from "vitest";

import {
	allowGuestTurnstile,
	allowTurnstile,
	renderHumanCheck,
	type TurnstileApi,
	type TurnstileOptions,
} from "./turnstile";

/** A stand-in for window.turnstile that records what the page asked of it. */
function fakeTurnstile() {
	const calls = { resets: [] as string[] };
	let options: TurnstileOptions | undefined;
	const api: TurnstileApi = {
		render: (_container, given) => {
			options = given;
			return "widget-1";
		},
		reset: (id) => void calls.resets.push(id),
	};
	return { api, calls, widget: () => options! };
}

const container = {} as HTMLElement;

describe("renderHumanCheck", () => {
	it("renders the form's action and follows the token", () => {
		const fake = fakeTurnstile();
		const check = renderHumanCheck(fake.api, container, {
			siteKey: "site-key",
			action: "signup-passkey",
			theme: "dark",
		});
		expect(fake.widget()).toMatchObject({
			sitekey: "site-key",
			action: "signup-passkey",
			theme: "dark",
		});
		expect(check.token()).toBeUndefined();
		expect(check.waiting()).toMatch(/has not finished/);

		fake.widget().callback("token-1");
		expect(check.token()).toBe("token-1");
		fake.widget()["expired-callback"]();
		expect(check.token()).toBeUndefined();
	});

	it("drops a used token and runs the widget again on reset", () => {
		const fake = fakeTurnstile();
		const check = renderHumanCheck(fake.api, container, {
			siteKey: "k",
			action: "signup-provider",
			theme: "light",
		});
		fake.widget().callback("token-1");
		check.reset();
		expect(check.token()).toBeUndefined();
		expect(fake.calls.resets).toEqual(["widget-1"]);
	});

	it("says so when the widget fails, until it passes again", () => {
		const fake = fakeTurnstile();
		const check = renderHumanCheck(fake.api, container, {
			siteKey: "k",
			action: "signup-provider",
			theme: "light",
		});
		fake.widget()["error-callback"]();
		expect(check.waiting()).toMatch(/could not run/);
		fake.widget().callback("token-2");
		expect(check.token()).toBe("token-2");
		expect(check.waiting()).toMatch(/has not finished/);
	});
});

/** A page's CSP API that records what was added. */
const page = () => {
	const added: string[] = [];
	const csp = {
		insertScriptResource: (resource: unknown) =>
			void added.push(`script-src ${String(resource)}`),
		insertDirective: (directive: string) => void added.push(directive),
	} as unknown as Parameters<typeof allowTurnstile>[0];
	return { csp, added };
};

describe("allowTurnstile", () => {
	it("allows Cloudflare's script and frame when the site key is set", () => {
		const { csp, added } = page();
		allowTurnstile(csp, "site-key");
		expect(added).toEqual([
			"script-src https://challenges.cloudflare.com",
			"frame-src https://challenges.cloudflare.com",
		]);
	});

	it("leaves the CSP alone while Turnstile is off", () => {
		const { csp, added } = page();
		allowTurnstile(csp, undefined);
		allowTurnstile(csp, "");
		expect(added).toEqual([]);
	});
});

describe("allowGuestTurnstile", () => {
	it("allows Cloudflare for signed-out visitors only", () => {
		const guest = page();
		allowGuestTurnstile(guest.csp, "site-key", false);
		expect(guest.added).toHaveLength(2);
		const member = page();
		allowGuestTurnstile(member.csp, "site-key", true);
		expect(member.added).toEqual([]);
	});
});
