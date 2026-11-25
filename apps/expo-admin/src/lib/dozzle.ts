// Container resource usage for the Overview, read from Dozzle (the logs
// tool, which already watches Docker through the read-only socket proxy).
// Dozzle has no stats API of its own; this reads what its UI reads: the
// host list from the page's embedded config and the first
// "containers-changed" event of /api/events/stream, which carries each
// container's recent CPU and memory samples.

/** One running container's latest numbers. */
export interface ContainerUsage {
	name: string;
	/** Percent of one core, as `docker stats` shows it (can pass 100). */
	cpu: number;
	/** Bytes in use. */
	memory: number;
	/** Percent of the container's limit, or of the host's memory without one. */
	memoryPercent: number;
}

export interface ResourceUsage {
	containers: ContainerUsage[];
	/** Every running container's CPU as a percent of the host's cores. */
	cpuPercent: number;
	/** Undefined when Dozzle did not say. */
	cores?: number | undefined;
	memoryUsed: number;
	memoryTotal?: number | undefined;
	/** Containers that exist but are not running (exited one-off jobs included). */
	stopped: number;
}

type Json = Record<string, unknown>;

const isObject = (value: unknown): value is Json => typeof value === "object" && value !== null;
const numbers = (value: unknown): number[] =>
	Array.isArray(value) ? value.filter((item): item is number => typeof item === "number") : [];
const numberOr0 = (value: unknown) => (typeof value === "number" ? value : 0);

/** The hosts in Dozzle's embedded page config; [] when not found. */
export function parseDozzleHosts(html: string): Json[] {
	const match = /<script[^>]*id="config__json"[^>]*>([\s\S]*?)<\/script>/.exec(html);
	if (!match?.[1]) return [];
	try {
		const config: unknown = JSON.parse(match[1]);
		if (!isObject(config) || !Array.isArray(config["hosts"])) return [];
		return config["hosts"].filter(isObject);
	} catch {
		return [];
	}
}

/** The data of the first `event` in a server-sent event stream, or undefined. */
export function findEvent(stream: string, event: string): string | undefined {
	for (const block of stream.split(/\r?\n\r?\n/)) {
		const lines = block.split(/\r?\n/);
		if (!lines.some((line) => line === `event: ${event}`)) continue;
		return lines
			.filter((line) => line.startsWith("data:"))
			.map((line) => line.slice(5).trimStart())
			.join("\n");
	}
	return undefined;
}

/**
 * Sums up a containers-changed event's containers (and Dozzle's hosts:
 * nCPU, memTotal). Each container's stats hold recent samples; memoryUsage
 * is sent as differences between samples, so its memory is their sum.
 */
export function summarize(containers: Json[], hosts: Json[]): ResourceUsage {
	const running = containers.filter((container) => container["state"] === "running");
	const usage = running
		.map((container) => {
			const stats = isObject(container["stats"]) ? container["stats"] : {};
			const name = container["name"];
			return {
				name: typeof name === "string" ? name : "?",
				cpu: numbers(stats["cpu"]).at(-1) ?? 0,
				memory: Math.max(
					0,
					numbers(stats["memoryUsage"]).reduce((sum, value) => sum + value, 0),
				),
				memoryPercent: numbers(stats["memory"]).at(-1) ?? 0,
			};
		})
		.toSorted((a, b) => b.cpu - a.cpu || b.memory - a.memory);
	const cores = hosts.reduce((sum, host) => sum + numberOr0(host["nCPU"]), 0) || undefined;
	const memoryTotal =
		hosts.reduce((sum, host) => sum + numberOr0(host["memTotal"]), 0) || undefined;
	const cpu = usage.reduce((sum, container) => sum + container.cpu, 0);
	return {
		containers: usage,
		cpuPercent: cores ? cpu / cores : cpu,
		cores,
		memoryUsed: usage.reduce((sum, container) => sum + container.memory, 0),
		memoryTotal,
		stopped: containers.length - running.length,
	};
}

const TIMEOUT_MS = 2_500;

/** Reads a stream until `event` arrives, then stops; undefined if it never does. */
async function readEvent(
	url: URL,
	event: string,
	signal: AbortSignal,
): Promise<string | undefined> {
	const response = await fetch(url, { headers: { Accept: "text/event-stream" }, signal });
	if (!response.ok || !response.body) return undefined;
	const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
	let text = "";
	try {
		for (;;) {
			// Reading a stream is sequential by nature.
			// oxlint-disable-next-line no-await-in-loop
			const { done, value } = await reader.read();
			if (done) return undefined;
			text += value;
			// An event is complete once the blank line after it arrived.
			const start = text.indexOf(`event: ${event}`);
			if (start >= 0 && /\r?\n\r?\n/.test(text.slice(start))) return findEvent(text, event);
		}
	} finally {
		await reader.cancel().catch(() => undefined);
	}
}

/** The host list (cores, memory) barely changes; it is read again after this. */
const HOSTS_TTL_MS = 10 * 60_000;
/** Polls from several open Overviews within this share one read. */
const USAGE_TTL_MS = 2_000;

let hostsCache: { baseUrl: string; at: number; hosts: Json[] } | undefined;
let usageCache:
	| { baseUrl: string; at: number; usage: Promise<ResourceUsage | undefined> }
	| undefined;

async function readHosts(baseUrl: string, signal: AbortSignal): Promise<Json[]> {
	if (hostsCache?.baseUrl === baseUrl && Date.now() - hostsCache.at < HOSTS_TTL_MS) {
		return hostsCache.hosts;
	}
	const response = await fetch(new URL("/", baseUrl), { signal });
	const hosts = response.ok ? parseDozzleHosts(await response.text()) : [];
	if (hosts.length > 0) hostsCache = { baseUrl, at: Date.now(), hosts };
	return hosts;
}

async function readUsage(baseUrl: string): Promise<ResourceUsage | undefined> {
	const signal = AbortSignal.timeout(TIMEOUT_MS);
	try {
		const [hosts, event] = await Promise.all([
			readHosts(baseUrl, signal).catch(() => []),
			readEvent(new URL("/api/events/stream", baseUrl), "containers-changed", signal),
		]);
		if (event === undefined) return undefined;
		const containers: unknown = JSON.parse(event);
		if (!Array.isArray(containers)) return undefined;
		return summarize(containers.filter(isObject), hosts);
	} catch (error) {
		console.warn("dozzle resource usage unavailable", error);
		return undefined;
	}
}

/** Resource usage from Dozzle at baseUrl; undefined when it cannot be read. */
export function readResourceUsage(baseUrl: string): Promise<ResourceUsage | undefined> {
	if (usageCache?.baseUrl === baseUrl && Date.now() - usageCache.at < USAGE_TTL_MS) {
		return usageCache.usage;
	}
	const usage = readUsage(baseUrl);
	usageCache = { baseUrl, at: Date.now(), usage };
	return usage;
}
