// Drives the e2e stack's Factorio containers for the recovery suite: crashes
// and restarts the server and the client, and follows their logs.
import { spawn, spawnSync } from "node:child_process";

export const SERVER = "factorio-expo-e2e-factorio-server";
export const CLIENT = "factorio-expo-e2e-factorio-client";

/** The client joined the game (see the watchdog in factorio/factorio-client/entrypoint.sh). */
export const CLIENT_IN_GAME =
	/ClientMultiplayerManager\.cpp:\d+: .*changing state from\(\w+\) to\(InGame\)/;
/** The client is stuck outside the game: the states its watchdog restarts it from. */
export const CLIENT_STUCK =
	/changing state from\(\w+\) to\((InitializationFailed|Disconnected|WaitingForUserToSaveOrQuitAfterServerLeft)\)/;

/** A bridge progress line (scripts/progress.lua) for any job, by its step. */
export const bridgeStep = (step: string) =>
	new RegExp(`Factorio Expo \\[pj_[\\w-]+\\] tick=\\d+ ${step}`);

function docker(...args: string[]): string {
	const result = spawnSync("docker", args, { encoding: "utf8" });
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`docker ${args.join(" ")} exited with ${result.status}: ${result.stderr}`);
	}
	return result.stdout;
}

/**
 * SIGKILLs the container's Factorio process, as an engine crash would; the
 * container keeps running and its entrypoint loop starts Factorio again.
 */
export function crashFactorio(container: string): void {
	docker(
		"exec",
		container,
		"sh",
		"-c",
		'for p in /proc/[0-9]*; do [ "$(cat "$p/comm" 2>/dev/null)" = factorio ] && kill -9 "${p#/proc/}"; done; true',
	);
}

/** Stops the container with SIGTERM and starts it again, as a deploy does. */
export function restartContainer(container: string): void {
	docker("restart", container);
}

export function stopContainer(container: string): void {
	docker("stop", container);
}

export function startContainer(container: string): void {
	docker("start", container);
}

/**
 * The client's latest multiplayer state from its current log: "" before the
 * first, "unavailable" while the container or its log is not there.
 */
export function clientState(): string {
	let log: string;
	try {
		log = docker("exec", CLIENT, "cat", "/app/data/factorio-current.log");
	} catch {
		return "unavailable";
	}
	const states = [...log.matchAll(/changing state from\(\w+\) to\((\w+)\)/g)];
	return states.at(-1)?.[1] ?? "";
}

/**
 * Resolves with the first line of the container's output since `since` that
 * matches `pattern`. Follows the log as it is written, so a step can be acted
 * on while it runs; a container restart ends `docker logs --follow`, which is
 * then started again.
 */
export function waitForLog(
	container: string,
	pattern: RegExp,
	since: Date,
	timeoutMs: number,
): Promise<string> {
	return new Promise((resolve, reject) => {
		let done = false;
		let child: ReturnType<typeof spawn> | undefined;
		const finish = (error: Error | undefined, line?: string) => {
			if (done) return;
			done = true;
			clearTimeout(timer);
			child?.kill();
			if (error) reject(error);
			else resolve(line ?? "");
		};
		const timer = setTimeout(
			() =>
				finish(
					new Error(
						`no line matching ${pattern} in ${container}'s log within ${timeoutMs / 1000} s`,
					),
				),
			timeoutMs,
		);
		const follow = () => {
			if (done) return;
			let pending = "";
			const onData = (chunk: Buffer) => {
				pending += chunk.toString("utf8");
				const lines = pending.split("\n");
				pending = lines.pop() ?? "";
				const line = lines.find((candidate) => pattern.test(candidate));
				if (line !== undefined) finish(undefined, line);
			};
			child = spawn("docker", [
				"logs",
				"--follow",
				"--since",
				since.toISOString(),
				container,
			]);
			child.stdout?.on("data", onData);
			child.stderr?.on("data", onData);
			child.on("error", (error) => finish(error));
			child.on("close", () => setTimeout(follow, 1000));
		};
		follow();
	});
}
