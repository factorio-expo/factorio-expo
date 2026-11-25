import { describe, expect, it } from "vitest";
import { inDisplayOrder } from "./provider-order";

describe("inDisplayOrder", () => {
	it("moves Steam last and keeps the rest in order", () => {
		const ids = ["steam", "github", "google", "discord"].map((id) => ({ id, label: id }));
		expect(inDisplayOrder(ids).map((p) => p.id)).toEqual([
			"github",
			"google",
			"discord",
			"steam",
		]);
	});
});
