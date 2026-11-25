// Arranges test data through the public API, the same door the site uses.
// Only for setup: the flow under test always goes through the browser.
import {
	expect,
	request,
	type APIRequestContext,
	type Browser,
	type Cookie,
	type Page,
} from "@playwright/test";
import { BLUEPRINT } from "./blueprints.ts";
import { useVirtualAuthenticator } from "./passkeys.ts";
import { apiURL, webURL } from "./stack.ts";

export type Account = {
	username: string;
	/** API client signed in as this account: a session of its own (another device). */
	api: APIRequestContext;
	/** The session cookie a sign-in leaves in the browser (not the API client's session). */
	session: Cookie[];
	/** The recovery code the sign-up showed; unused until a test recovers the account. */
	recoveryCode: string;
};

export type SeededEntry = {
	id: string;
	title: string;
	/** Anonymous entries only. */
	editToken?: string;
};

export type SeededCollection = { id: string; name: string };

/** Short random word: unique per test and a single search token. */
export function uniqueWord(prefix = "e2e"): string {
	return `${prefix}${Date.now().toString(36).slice(-4)}${Math.random().toString(36).slice(2, 8)}`;
}

// Writes must carry an allowed Origin (the API's CSRF check), as the browser's do.
const clientOptions = { baseURL: apiURL, extraHTTPHeaders: { Origin: webURL } };

async function json<T>(response: Awaited<ReturnType<APIRequestContext["get"]>>): Promise<T> {
	expect(
		response.ok(),
		`${response.url()} answered ${response.status()}: ${await response.text()}`,
	).toBe(true);
	return (await response.json()) as T;
}

export class Seeder {
	private readonly clients: APIRequestContext[] = [];
	private readonly browser: Browser;

	constructor(browser: Browser) {
		this.browser = browser;
	}

	private async client(): Promise<APIRequestContext> {
		const client = await request.newContext(clientOptions);
		this.clients.push(client);
		return client;
	}

	/**
	 * Creates an account through the real passkey sign-up (a throwaway browser
	 * context with a virtual authenticator), then logs in once more with the
	 * same passkey: the sign-up's session goes to the API client, the second
	 * one to `session`, so the two are different devices, as for a person
	 * signed in on a phone and a laptop.
	 */
	async account(): Promise<Account> {
		const username = uniqueWord("user_");
		const context = await this.browser.newContext({ baseURL: webURL });
		try {
			const page = await context.newPage();
			await useVirtualAuthenticator(page);
			await page.goto("/about");
			const ceremony = (kind: "signup" | "login") =>
				page.evaluate(
					async ({ api, name, step }) => {
						const post = async (path: string, body?: unknown) => {
							const response = await fetch(api + path, {
								method: "POST",
								credentials: "include",
								headers: { "Content-Type": "application/json" },
								body: JSON.stringify(body ?? {}),
							});
							if (!response.ok) {
								throw new Error(
									`${path} answered ${response.status}: ${await response.text()}`,
								);
							}
							return response.json();
						};
						let credential: Credential | null;
						if (step === "signup") {
							const options = await post("/api/v1/auth/passkeys/signup/begin", {
								username: name,
							});
							credential = await navigator.credentials.create({
								publicKey:
									PublicKeyCredential.parseCreationOptionsFromJSON(options),
							});
						} else {
							const options = await post("/api/v1/auth/passkeys/login/begin");
							credential = await navigator.credentials.get({
								publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options),
							});
						}
						if (!(credential instanceof PublicKeyCredential))
							throw new Error(`no passkey for the ${step}`);
						const finished: { recovery_code?: string } = await post(
							`/api/v1/auth/passkeys/${step}/finish`,
							credential.toJSON(),
						);
						return finished.recovery_code ?? "";
					},
					{ api: apiURL, name: username, step: kind },
				);
			const sessionCookie = async () => {
				const cookies = (await context.cookies(apiURL)).filter((cookie) =>
					cookie.name.endsWith("expo_session"),
				);
				expect(cookies, "the passkey ceremony set no session cookie").toHaveLength(1);
				return cookies;
			};
			const recoveryCode = await ceremony("signup");
			expect(recoveryCode, "the sign-up gave no recovery code").not.toBe("");
			const apiSession = await sessionCookie();
			await ceremony("login");
			const session = await sessionCookie();
			expect(session[0]?.value, "the login reused the sign-up's session").not.toBe(
				apiSession[0]?.value,
			);
			const api = await request.newContext({
				...clientOptions,
				storageState: { cookies: apiSession, origins: [] },
			});
			this.clients.push(api);
			return { username, api, session, recoveryCode };
		} finally {
			await context.close();
		}
	}

	/** Creates an entry anonymously, or owned by `owner`. */
	async entry(
		options: {
			title?: string;
			description?: string;
			blueprint?: string;
			tags?: string[];
			visibility?: "public" | "unlisted" | "private";
			/** Marks it spoiler/NSFW. */
			spoiler?: boolean;
			/** The vanity alias (members only). */
			alias?: string;
			owner?: Account;
		} = {},
	): Promise<SeededEntry> {
		const api = options.owner?.api ?? (await this.client());
		const title = options.title ?? `Entry ${uniqueWord()}`;
		const entry = await json<{ id: string; edit_token?: string }>(
			await api.post("/api/v1/entries", {
				data: {
					blueprint: options.blueprint ?? BLUEPRINT,
					title,
					description: options.description ?? "",
					tags: options.tags ?? [],
					visibility: options.visibility,
					spoiler: options.spoiler,
					alias: options.alias,
				},
			}),
		);
		return { id: entry.id, title, editToken: entry.edit_token };
	}

	/** Deletes the entry as its owner or with its edit token. */
	async deleteEntry(entry: SeededEntry, owner?: Account): Promise<void> {
		const api = owner?.api ?? (await this.client());
		const response = await api.delete(`/api/v1/entries/${entry.id}`, {
			headers: entry.editToken ? { "X-Edit-Token": entry.editToken } : {},
		});
		expect(response.ok(), `delete entry answered ${response.status()}`).toBe(true);
	}

	/** Adds a revision (with an optional note) as the entry's owner or with its edit token. */
	async revision(
		entry: SeededEntry,
		blueprint: string,
		owner?: Account,
		note?: string,
	): Promise<void> {
		const api = owner?.api ?? (await this.client());
		const response = await api.post(`/api/v1/entries/${entry.id}/revisions`, {
			data: { blueprint, note },
			headers: entry.editToken ? { "X-Edit-Token": entry.editToken } : {},
		});
		expect(
			response.ok(),
			`new revision answered ${response.status()}: ${await response.text()}`,
		).toBe(true);
	}

	async collection(
		owner: Account,
		options: { name?: string; description?: string; visibility?: "public" | "private" } = {},
	): Promise<SeededCollection> {
		const name = options.name ?? `Collection ${uniqueWord()}`;
		const collection = await json<{ id: string }>(
			await owner.api.post("/api/v1/collections", {
				data: {
					name,
					description: options.description ?? "",
					visibility: options.visibility ?? "private",
				},
			}),
		);
		return { id: collection.id, name };
	}

	async addToCollection(
		owner: Account,
		collection: SeededCollection,
		entry: SeededEntry,
	): Promise<void> {
		const response = await owner.api.post(`/api/v1/collections/${collection.id}/entries`, {
			data: { entry_id: entry.id },
		});
		expect(response.ok(), `add to collection answered ${response.status()}`).toBe(true);
	}

	async favorite(owner: Account, entry: SeededEntry): Promise<void> {
		const response = await owner.api.post(`/api/v1/entries/${entry.id}/favorite`);
		expect(response.ok(), `favorite answered ${response.status()}`).toBe(true);
	}

	/** Posts a comment on the entry as `author`; returns its number. */
	async comment(author: Account, entry: SeededEntry, body: string): Promise<number> {
		const comment = await json<{ number: number }>(
			await author.api.post(`/api/v1/entries/${entry.id}/comments`, { data: { body } }),
		);
		return comment.number;
	}

	/** Enters the owner's entry in the challenge. */
	async challengeEntry(owner: Account, slug: string, entry: SeededEntry): Promise<void> {
		const response = await owner.api.post(`/api/v1/challenges/${slug}/entry`, {
			data: { entry_id: entry.id },
		});
		expect(response.ok(), `challenge entry answered ${response.status()}`).toBe(true);
	}

	/** Gives the voter's vote to the entry in the challenge. */
	async challengeVote(voter: Account, slug: string, entry: SeededEntry): Promise<void> {
		const response = await voter.api.put(`/api/v1/challenges/${slug}/votes/${entry.id}`);
		expect(
			response.ok(),
			`challenge vote answered ${response.status()}: ${await response.text()}`,
		).toBe(true);
	}

	/** Waits until search (a projection that lags writes) returns the entry. */
	async searchable(entry: SeededEntry, query = entry.title): Promise<void> {
		const api = await this.client();
		await expect
			.poll(
				async () => {
					const response = await api.get("/api/v1/search", { params: { q: query } });
					const body = (await response.json()) as { results?: { entry_id: string }[] };
					return body.results?.some((result) => result.entry_id === entry.id) ?? false;
				},
				{ message: `entry ${entry.id} never became searchable`, timeout: 30_000 },
			)
			.toBe(true);
	}

	async dispose(): Promise<void> {
		await Promise.all(this.clients.map((client) => client.dispose()));
	}
}

/**
 * Signs the page's browser in as `account` without the login page: the
 * session cookie is all a real sign-in leaves in the browser.
 */
export async function signIn(page: Page, account: Account): Promise<void> {
	await page.context().addCookies(account.session);
}
