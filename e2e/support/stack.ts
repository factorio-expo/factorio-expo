// The e2e compose project: the root docker-compose.yaml plus e2e/compose.yaml,
// run as its own project so it never touches the dev stack.
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const e2eDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(e2eDir, "..");

// Same defaults as e2e.env / compose.yaml; an exported variable overrides both.
export const webPort = Number(process.env.E2E_WEB_PORT ?? 3100);
export const apiPort = Number(process.env.E2E_API_PORT ?? 8180);
export const webURL = `http://localhost:${webPort}`;
export const apiURL = `http://localhost:${apiPort}`;

const project = "factorio-expo-e2e";

/** E2E_FACTORIO=1 adds Factorio (factorio.compose.yaml), for the recovery suite. */
const withFactorio = () => process.env.E2E_FACTORIO === "1";
/** factorio.compose.yaml's own volume; external, so a fresh stack keeps it. */
const clientCacheVolume = "factorio-expo-e2e-client-cache";

export function compose(...args: string[]): void {
	const factorio = withFactorio() ? ["--file", path.join(e2eDir, "factorio.compose.yaml")] : [];
	const result = spawnSync(
		"docker",
		[
			"compose",
			"--project-name",
			project,
			"--env-file",
			path.join(e2eDir, "e2e.env"),
			"--file",
			path.join(repoRoot, "docker-compose.yaml"),
			"--file",
			path.join(e2eDir, "compose.yaml"),
			...factorio,
			...args,
		],
		{ cwd: repoRoot, stdio: "inherit" },
	);
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`docker compose ${args.join(" ")} exited with ${result.status}`);
	}
}

/** Builds and starts a fresh stack: the old one, and its tmpfs database, go first. */
export function stackUp(): void {
	stackDown();
	if (withFactorio()) {
		const result = spawnSync("docker", ["volume", "create", clientCacheVolume], {
			stdio: "ignore",
		});
		if (result.status !== 0)
			throw new Error(`could not create the ${clientCacheVolume} volume`);
	}
	compose("up", "--detach", "--build", "--wait");
}

/** Removes the stack, with or without Factorio: the profile names its services either way. */
export function stackDown(): void {
	compose("--profile", "factorio", "down", "--volumes", "--remove-orphans");
}

/** Waits until the site renders and the API answers, so the first test does not race startup. */
export async function waitForStack(timeoutMs = 120_000): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	let lastError = "";
	for (const url of [`${apiURL}/healthz`, `${webURL}/`]) {
		for (;;) {
			try {
				const response = await fetch(url);
				if (response.ok) break;
				lastError = `${url} answered ${response.status}`;
			} catch (err) {
				lastError = `${url}: ${(err as Error).message}`;
			}
			if (Date.now() > deadline) throw new Error(`e2e stack is not ready: ${lastError}`);
			await new Promise((resolve) => setTimeout(resolve, 1000));
		}
	}
}

/**
 * Freezes the render worker (`docker compose pause`) until the returned
 * function is called: the API's dispatches to it hang, so a new entry stays
 * pending for as long as a test needs. Other tests only see slower dispatch.
 */
export function pauseRenderWorker(): () => void {
	compose("pause", "expo-render-worker");
	let paused = true;
	return () => {
		if (paused) compose("unpause", "expo-render-worker");
		paused = false;
	};
}

/**
 * Moves every session of `username` an hour past its login, so the next
 * sign-in method change asks "Confirm it's you" (the API wants a login from
 * the last 10 minutes). Straight in the database: no API can age a login.
 */
export function ageSignIns(username: string): void {
	catalogSQL(`UPDATE user_sessions SET auth_time = now() - interval '1 hour'
		 WHERE user_id = (SELECT id FROM users WHERE username = '${checkedUsername(username)}')`);
}

/**
 * Bans `username` from commenting for a day, as staff would from the console
 * (which the e2e stack does not run).
 */
export function banAccount(username: string): void {
	catalogSQL(`INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by, expires_at)
		 SELECT substr(md5(random()::text), 1, 12), id, ARRAY['comment'], 'spam', 'e2e', now() + interval '1 day'
		 FROM users WHERE username = '${checkedUsername(username)}'`);
}

/**
 * Hard-deletes the deleted account `username` (with its bans), as the daily
 * sweep does 7 days after the deletion; the account must own no entries.
 */
export function purgeDeletedAccount(username: string): void {
	catalogSQL(`DELETE FROM users
		 WHERE username = '${checkedUsername(username)}' AND deleted_at IS NOT NULL`);
}

/** Where a challenge is in its timeline (catalog.challengeStatus). */
export type ChallengePhase = "upcoming" | "open" | "judging" | "closed";

/** A challenge's settings (the console's editor), as `createChallenge` stores them. */
export interface ChallengeSettings {
	phase?: ChallengePhase;
	/** Unpublished: a draft is never public. */
	draft?: boolean;
	/** challenge.Rules fields; the default is the smallest footprint, any kind, Space Age. */
	rules?: {
		content?: "vanilla" | "space-age";
		kind?: "any" | "factory" | "platform";
		goal?: { param: string; target?: Record<string, string>; direction: "max" | "min" };
		restrictions?: {
			param: string;
			target?: Record<string, string>;
			op: "<" | "<=" | "=" | ">=" | ">";
			value: number;
		}[];
	};
	description?: string;
	requireNewRevisions?: boolean;
	votersBeforeStart?: boolean;
	votingEnabled?: boolean;
	votesPerUser?: number;
	liveLeaderboard?: boolean;
	hiddenUntilStart?: boolean;
}

/**
 * The phase's start, deadline and voting end, relative to now. Past phases
 * end a moment ago, so revisions uploaded before the call still count
 * (after the deadline a challenge scores the newest revision made before it).
 */
const phaseTimes: Record<ChallengePhase, [string, string, string]> = {
	upcoming: [
		"now() + interval '1 day'",
		"now() + interval '3 days'",
		"now() + interval '5 days'",
	],
	open: ["now() - interval '1 hour'", "now() + interval '2 days'", "now() + interval '3 days'"],
	judging: [
		"now() - interval '1 hour'",
		"now() - interval '1 second'",
		"now() + interval '1 day'",
	],
	closed: [
		"now() - interval '1 hour'",
		"now() - interval '2 seconds'",
		"now() - interval '1 second'",
	],
};

/**
 * Publishes a challenge named by `slug`, as staff would from the console
 * (which the e2e stack does not run): open by default, the goal the smallest
 * footprint, anyone may vote, 3 votes each, a live leaderboard.
 */
export function createChallenge(
	slug: string,
	title: string,
	settings: ChallengeSettings = {},
): void {
	if (!/^[A-Za-z0-9 ]+$/.test(title)) throw new Error(`not a plain title: ${title}`);
	const rules = {
		content: settings.rules?.content ?? "space-age",
		kind: settings.rules?.kind ?? "any",
		goal: settings.rules?.goal ?? { param: "footprint_area", target: {}, direction: "min" },
		restrictions: (settings.rules?.restrictions ?? []).map((r) => ({
			param: r.param,
			target: r.target ?? {},
			op: r.op,
			value: r.value,
		})),
	};
	const [starts, ends, votingEnds] = phaseTimes[settings.phase ?? "open"];
	catalogSQL(`INSERT INTO challenges (id, slug, title, summary, description, rules, starts_at,
		 ends_at, voting_ends_at, require_new_revisions, voters_before_start, voting_enabled,
		 votes_per_user, live_leaderboard, hidden_until_start, published_at)
		 VALUES (substr(md5(random()::text), 1, 12), '${checkedSlug(slug)}', '${title}', 'Smallest wins',
		 ${literal(settings.description ?? "")}, ${literal(JSON.stringify(rules))},
		 ${starts}, ${ends}, ${votingEnds},
		 ${settings.requireNewRevisions ?? true}, ${settings.votersBeforeStart ?? false},
		 ${settings.votingEnabled ?? true}, ${settings.votesPerUser ?? 3},
		 ${settings.liveLeaderboard ?? true}, ${settings.hiddenUntilStart ?? false},
		 ${settings.draft ? "NULL" : "now()"})`);
}

/**
 * Moves the challenge into `phase` now, as time would: an open challenge's
 * entries stay entered, its deadline passes, then its voting ends.
 */
export function setChallengePhase(slug: string, phase: ChallengePhase): void {
	const [starts, ends, votingEnds] = phaseTimes[phase];
	const start = phase === "upcoming" ? starts : `least(starts_at, ${starts})`;
	catalogSQL(`UPDATE challenges SET starts_at = ${start}, ends_at = ${ends},
		 voting_ends_at = ${votingEnds} WHERE slug = '${checkedSlug(slug)}'`);
}

/** A space platform's metrics: top speed (km/s), thrust (MN) and weight (t). */
export interface Ship {
	speed: number;
	thrust: number;
	weight: number;
}

/** The analysis fixture's stored snapshot sections (analysisresult.snapshotKeys). */
const snapshotKeys = [
	"ingredients",
	"products",
	"intermediates",
	"electricity",
	"pollution",
	"completeness",
	"calculator",
	"space_platform",
	"footprint",
	"build_cost",
];

/** The stored blueprint of the entry's current revision (a single blueprint). */
const currentBlueprint = (entryId: string) =>
	`(SELECT revisions.tree->>'id' FROM entries
	  JOIN revisions ON revisions.id = entries.current_revision_id
	  WHERE entries.id = '${checkedID(entryId)}')`;

/**
 * Stores a finished analysis for the entry's current blueprint, as the API
 * does when the render worker reports one (the e2e stack runs no Factorio):
 * test-data/analysis/analysis-succeeded.json with the given footprint, so a
 * footprint goal scores width x height. `partial` stores it incomplete.
 * `rate` multiplies the products' rates (the fixture makes 0.5/s of electronic
 * circuits), and `platform` makes it a space platform: `true` a slow 100 t
 * ship (30 km/s, 3 MN), or a ship of its own metrics. `science` makes it a
 * science build instead: its only product that many automation science packs
 * a second (the factory leaderboard boards).
 */
export function storeAnalysis(
	entryId: string,
	result: {
		width: number;
		height: number;
		partial?: boolean;
		rate?: number;
		platform?: boolean | Ship;
		science?: number;
	},
): void {
	const fixture = JSON.parse(
		readFileSync(path.join(repoRoot, "test-data/analysis/analysis-succeeded.json"), "utf8"),
	) as Record<string, unknown>;
	const snapshot = Object.fromEntries(snapshotKeys.map((key) => [key, fixture[key]]));
	snapshot.footprint = { width: result.width, height: result.height };
	if (result.rate !== undefined) {
		const products = fixture.products as { output_per_second: number }[];
		snapshot.products = products.map((product) =>
			Object.assign({}, product, {
				output_per_second: product.output_per_second * result.rate!,
			}),
		);
	}
	if (result.science !== undefined) {
		snapshot.products = [
			{
				type: "item",
				name: "automation-science-pack",
				quality: "normal",
				output_per_second: result.science,
				producer_count: 1,
			},
		];
	}
	if (result.platform) {
		const ship =
			result.platform === true ? { speed: 30, thrust: 3, weight: 100 } : result.platform;
		snapshot.space_platform = {
			is_space_platform: true,
			weight_tons: ship.weight,
			thrust_mn: ship.thrust,
			max_speed_km_s: ship.speed,
		};
	}
	const state = result.partial ? "partial" : "complete";
	catalogSQL(`UPDATE blueprint_stats SET production = ${literal(JSON.stringify(snapshot))}::jsonb,
		 production_schema_version = 1, production_state = '${state}',
		 production_stale = false, production_calculated_at = now()
		 WHERE blueprint_id = ${currentBlueprint(entryId)};
		 DELETE FROM phase_jobs WHERE phase = 'analysis' AND blueprint_id = ${currentBlueprint(entryId)}`);
}

/**
 * Queues a search reindex of the entry, as the API does after an analysis
 * import (`storeAnalysis` writes the database directly and so queues none).
 */
export function reindexEntry(entryId: string): void {
	catalogSQL(`INSERT INTO entry_search_jobs (entry_id) VALUES ('${checkedID(entryId)}')
		 ON CONFLICT (entry_id) DO UPDATE
		 SET action = 'index', state = 'pending', available_at = now(), updated_at = now()`);
}

/** Moves the entry's upload date, then reindexes it (the date range filter). */
export function setEntryCreatedAt(entryId: string, createdAt: string): void {
	catalogSQL(`UPDATE entries SET created_at = ${literal(createdAt)}::timestamptz
		 WHERE id = '${checkedID(entryId)}'`);
	reindexEntry(entryId);
}

/** Fails the entry's analysis for good, as a worker failure past its retries does. */
export function failAnalysis(entryId: string): void {
	catalogSQL(`UPDATE phase_jobs SET state = 'failed', failure_code = 'e2e',
		 failure_detail = 'failed by the e2e suite'
		 WHERE phase = 'analysis' AND blueprint_id = ${currentBlueprint(entryId)}`);
}

/** Overrides the rule check of the entry's submission, as staff do in the console. */
export function overrideChallengeEntry(
	slug: string,
	entryId: string,
	verdict: "accepted" | "rejected",
	reason: string,
): void {
	catalogSQL(`UPDATE challenge_submissions
		 SET staff_verdict = '${verdict}', staff_reason = ${literal(reason)}
		 WHERE entry_id = '${checkedID(entryId)}' AND withdrawn_at IS NULL
		   AND challenge_id = (SELECT id FROM challenges WHERE slug = '${checkedSlug(slug)}')`);
}

/**
 * Backdates the entry's revisions a day, so they count as uploaded before a
 * challenge that started an hour ago (revisions are never updated otherwise).
 */
export function ageRevisions(entryId: string): void {
	catalogSQL(`UPDATE revisions SET created_at = created_at - interval '1 day'
		 WHERE entry_id = '${checkedID(entryId)}'`);
}

/** Backdates the account's sign-up a day, so it counts as made before a challenge started. */
export function ageAccount(username: string): void {
	catalogSQL(`UPDATE users SET created_at = now() - interval '1 day'
		 WHERE username = '${checkedUsername(username)}'`);
}

/**
 * Makes the entry's current blueprint the home page's only pick, with a
 * caption, as staff would in the console (which the e2e stack does not run).
 */
export function pickForHome(entryId: string, caption: string): void {
	catalogSQL(`DELETE FROM home_picks;
		 INSERT INTO home_picks (blueprint_id, caption, picked_by)
		 VALUES (${currentBlueprint(entryId)}, ${literal(caption)}, 'e2e')`);
}

/** A string as an SQL literal: dollar-quoted, so quotes inside need no escaping. */
function literal(text: string): string {
	if (text.includes("$e2e$")) throw new Error("text holds the quote tag");
	return `$e2e$${text}$e2e$`;
}

function checkedSlug(slug: string): string {
	if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(slug)) throw new Error(`not a slug: ${slug}`);
	return slug;
}

function checkedID(id: string): string {
	if (!/^[A-Za-z0-9_-]{1,32}$/.test(id)) throw new Error(`not an id: ${id}`);
	return id;
}

/** A username safe to put in SQL (the API's own username rule). */
function checkedUsername(username: string): string {
	if (!/^[A-Za-z0-9_-]{3,32}$/.test(username)) throw new Error(`not a username: ${username}`);
	return username;
}

/** Runs one statement in the stack's catalog database. */
function catalogSQL(command: string): void {
	compose(
		"exec",
		"-T",
		"expo-postgres",
		"psql",
		"--quiet",
		"--username=factorio_expo",
		"--dbname=factorio_expo",
		"--set=ON_ERROR_STOP=1",
		"--command",
		command,
	);
}
