import { describe, expect, it } from "vitest";

import { findEvent, parseDozzleHosts, summarize } from "./dozzle";

describe("dozzle resource usage", () => {
	it("reads the hosts from the page config", () => {
		const html =
			'<head><script type="application/json" id="config__json">{"hosts":[{"nCPU":12,"memTotal":1000}]}</script>';
		expect(parseDozzleHosts(html)).toEqual([{ nCPU: 12, memTotal: 1000 }]);
		expect(parseDozzleHosts("<html></html>")).toEqual([]);
		expect(parseDozzleHosts('<script id="config__json">{oops</script>')).toEqual([]);
	});

	it("finds one event's data in a stream", () => {
		const stream =
			'retry: 3000\n\nevent: server-version\ndata: {"version":"v11"}\n\nevent: containers-changed\ndata: [1]\n\n';
		expect(findEvent(stream, "containers-changed")).toBe("[1]");
		expect(findEvent(stream, "container-stat")).toBeUndefined();
	});

	it("sums memory deltas and shares CPU over the host's cores", () => {
		const usage = summarize(
			[
				{
					name: "api",
					state: "running",
					stats: { cpu: [5, 50], memory: [1, 2], memoryUsage: [300, -100, 50] },
				},
				{
					name: "web",
					state: "running",
					stats: { cpu: [10], memory: [3], memoryUsage: [400] },
				},
				{ name: "migrate", state: "exited" },
			],
			[{ nCPU: 4, memTotal: 10_000 }],
		);
		expect(usage.containers.map((container) => [container.name, container.memory])).toEqual([
			["api", 250],
			["web", 400],
		]);
		expect(usage.cpuPercent).toBe(15);
		expect(usage.memoryUsed).toBe(650);
		expect(usage.memoryTotal).toBe(10_000);
		expect(usage.stopped).toBe(1);
	});
});
