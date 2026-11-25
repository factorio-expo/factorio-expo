import { describe, expect, it } from "vitest";
import { pickedBlueprintId } from "./home-picks";

describe("pickedBlueprintId", () => {
	it("reads a bare id or a blueprint page link", () => {
		expect(pickedBlueprintId(" abcDEF123456 ")).toBe("abcDEF123456");
		expect(
			pickedBlueprintId("https://expo.example/e/entryID12345/b/leafID123456?token=x#y"),
		).toBe("leafID123456");
		expect(pickedBlueprintId("/e/entryID12345/b/leafID123456")).toBe("leafID123456");
	});

	it("refuses anything else", () => {
		expect(pickedBlueprintId("https://expo.example/e/entryID12345")).toBeUndefined();
		expect(pickedBlueprintId("short")).toBeUndefined();
		expect(pickedBlueprintId("")).toBeUndefined();
		expect(pickedBlueprintId("/e/entryID12345/b/leafID123456/extra")).toBeUndefined();
	});
});
