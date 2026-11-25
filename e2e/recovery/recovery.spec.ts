// Factorio fails, the stack heals: the server or the client crashes (SIGKILL)
// or restarts (SIGTERM), while idle and while a job runs. After each, the
// client is back in the game on its own and a blueprint uploaded around the
// failure gets its preview and analysis through the public API, sooner than
// the runner's 5 minute job timeout would allow. The one exception is the job
// running when the server goes down: the blueprint most likely crashed it, so
// it fails for good (factorio_restarted) instead of crashing it again.
import { expect, request, test, type APIRequestContext } from "@playwright/test";
import { Seeder, uniqueWord } from "../support/api.ts";
import { BLUEPRINT, PLATFORM, relabel } from "../support/blueprints.ts";
import { apiURL } from "../support/stack.ts";
import {
	CLIENT,
	CLIENT_IN_GAME,
	CLIENT_STUCK,
	SERVER,
	bridgeStep,
	clientState,
	crashFactorio,
	restartContainer,
	startContainer,
	stopContainer,
	waitForLog,
} from "./factorio.ts";

const minutes = (count: number) => count * 60_000;

let seed: Seeder;
let api: APIRequestContext;

test.beforeAll(async ({ browser }) => {
	seed = new Seeder(browser);
	api = await request.newContext({ baseURL: apiURL });
});

test.afterAll(async () => {
	await api.dispose();
	await seed.dispose();
});

// Every test starts from a client in the game. A test that failed half way
// must not leave the server stopped for the next one.
test.beforeEach(async () => {
	startContainer(SERVER);
	await expect.poll(clientState, { timeout: minutes(4), intervals: [2000] }).toBe("InGame");
});

/** Uploads a blueprint anonymously; returns its entry id. */
async function upload(blueprint: string): Promise<string> {
	const entry = await seed.entry({ blueprint: relabel(blueprint, uniqueWord("recovery")) });
	return entry.id;
}

/** " (code)" for a failure code, "" for none. */
const code = (failure?: string) => (failure ? ` (${failure})` : "");

/** Waits until the entry's one leaf reaches `state`, e.g. "preview ready, analysis ready". */
async function expectLeaf(entryId: string, state: string, timeout: number): Promise<void> {
	const entry = await (await api.get(`/api/v1/entries/${entryId}`)).json();
	const leaf: string = entry.revision.root.id;
	await expect
		.poll(
			async () => {
				const detail = await (
					await api.get(`/api/v1/entries/${entryId}/blueprints/${leaf}`)
				).json();
				return (
					`preview ${detail.preview_status}${code(detail.preview_failure_code)}, ` +
					`analysis ${detail.production.availability}${code(detail.production.failure_code)}`
				);
			},
			{ timeout, intervals: [2000] },
		)
		.toBe(state);
}

/** Waits until the entry's one leaf has a preview and an analysis. */
async function expectRendered(entryId: string, timeout: number): Promise<void> {
	await expectLeaf(entryId, "preview ready, analysis ready", timeout);
}

const failures = {
	crash: crashFactorio,
	restart: restartContainer,
};

test.describe("idle", () => {
	for (const [failure, fail] of Object.entries(failures)) {
		test(`server ${failure}: the client rejoins and jobs run again`, async () => {
			const failedAt = new Date();
			fail(SERVER);
			// The client notices ~30 s later and is stuck until its watchdog restarts it.
			await waitForLog(CLIENT, CLIENT_STUCK, failedAt, minutes(2));
			await waitForLog(CLIENT, CLIENT_IN_GAME, failedAt, minutes(3));
			await expectRendered(await upload(BLUEPRINT), minutes(3));
		});

		test(`client ${failure}: it rejoins and jobs run again`, async () => {
			const failedAt = new Date();
			fail(CLIENT);
			await waitForLog(CLIENT, CLIENT_IN_GAME, failedAt, minutes(3));
			await expectRendered(await upload(BLUEPRINT), minutes(3));
		});
	}

	test("server down for a while: the client keeps retrying and joins once it is back", async () => {
		const stoppedAt = new Date();
		stopContainer(SERVER);
		await waitForLog(CLIENT, CLIENT_STUCK, stoppedAt, minutes(2));
		// Restarted while the server's host does not resolve: it cannot join either.
		await waitForLog(
			CLIENT,
			/changing state from\(\w+\) to\(InitializationFailed\)/,
			stoppedAt,
			minutes(3),
		);
		const entryId = await upload(BLUEPRINT);
		const startedAt = new Date();
		startContainer(SERVER);
		await waitForLog(CLIENT, CLIENT_IN_GAME, startedAt, minutes(3));
		await expectRendered(entryId, minutes(3));
	});
});

test.describe("during a job", () => {
	for (const [failure, fail] of Object.entries(failures)) {
		test(`server ${failure}: the job fails for good and the next one finishes`, async () => {
			const uploadedAt = new Date();
			const entryId = await upload(PLATFORM);
			await waitForLog(SERVER, bridgeStep("prepare_job: starting"), uploadedAt, minutes(3));
			fail(SERVER);
			// The restarted server forgot the job; the runner fails it as soon as
			// the bridge answers, not retryable, so the API does not run it again.
			const lost =
				"preview failed (factorio_restarted), analysis failed (factorio_restarted)";
			await expectLeaf(entryId, lost, minutes(4));
			await expectRendered(await upload(BLUEPRINT), minutes(4));
		});
	}

	test("client crash after the capture: only the preview runs again and finishes", async () => {
		const uploadedAt = new Date();
		const entryId = await upload(PLATFORM);
		await waitForLog(
			SERVER,
			bridgeStep("run_preview: capture requested"),
			uploadedAt,
			minutes(3),
		);
		const failedAt = new Date();
		crashFactorio(CLIENT);
		// The screenshot died with the client; the analysis was written by the server.
		await waitForLog(
			SERVER,
			bridgeStep(String.raw`prepare_job: starting \(requested_phases=preview\)`),
			failedAt,
			minutes(4),
		);
		await expectRendered(entryId, minutes(4));
	});
});
