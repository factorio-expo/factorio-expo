// The stand-in server of the sites the API imports from (e2e/import-stub):
// specs register what a site answers, then import a link to that site. The
// API reaches it inside the stack (API_IMPORT_STUB_ORIGIN, every adapter under
// /<adapter name>); specs reach it on the loopback port.
import { expect, request } from "@playwright/test";

export const importStubPort = Number(process.env.E2E_IMPORT_STUB_PORT ?? 8190);
const stubURL = `http://localhost:${importStubPort}`;

export type StubResponse = {
	status?: number;
	contentType?: string;
	body: string;
};

/** Makes the stub answer `path` (the adapter's request path, e.g. "/pastebin/raw/<id>") this way. */
export async function stubSite(path: string, response: StubResponse): Promise<void> {
	const client = await request.newContext({ baseURL: stubURL });
	try {
		const answer = await client.put("/__stub", { data: { path, ...response } });
		expect(answer.ok(), `stub answered ${answer.status()}`).toBe(true);
	} finally {
		await client.dispose();
	}
}

/** Every request the stub received (path and query), oldest first. */
export async function stubRequests(): Promise<string[]> {
	const client = await request.newContext({ baseURL: stubURL });
	try {
		return (await (await client.get("/__log")).json()) as string[];
	} finally {
		await client.dispose();
	}
}
