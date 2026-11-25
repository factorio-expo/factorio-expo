import { describe, expect, it } from "vitest";
import {
	centerOn,
	clampView,
	fitView,
	panBy,
	pinchView,
	toFraction,
	toStage,
	zoomAt,
	zoomLimits,
} from "./panzoom";

const stage = { w: 800, h: 600 };
const image = { w: 4000, h: 3000 };

describe("zoomLimits", () => {
	it("fits a large image and allows four screen pixels per image pixel", () => {
		expect(zoomLimits(stage, image)).toEqual({ min: 0.2, max: 4 });
	});

	it("never enlarges a small image past its own pixels when fitting", () => {
		expect(zoomLimits(stage, { w: 100, h: 50 }).min).toBe(1);
	});
});

describe("fitView", () => {
	it("centres the whole image", () => {
		expect(fitView(stage, image)).toEqual({ k: 0.2, x: 0, y: 0 });
		expect(fitView(stage, { w: 400, h: 300 })).toEqual({ k: 1, x: 200, y: 150 });
	});
});

describe("clampView", () => {
	it("keeps a zoomed image covering the stage", () => {
		const view = clampView({ k: 1, x: 100, y: -99999 }, stage, image);
		expect(view).toEqual({ k: 1, x: 0, y: 600 - 3000 });
	});

	it("limits the zoom", () => {
		expect(clampView({ k: 99, x: 0, y: 0 }, stage, image).k).toBe(4);
		expect(clampView({ k: 0.001, x: 0, y: 0 }, stage, image).k).toBe(0.2);
	});
});

describe("zoomAt", () => {
	it("keeps the image point under the cursor", () => {
		const start = { k: 1, x: -500, y: -400 };
		const at = { x: 300, y: 200 };
		const before = toFraction(start, at, image);
		const zoomed = zoomAt(start, 2, at, stage, image);
		expect(zoomed.k).toBe(2);
		const after = toFraction(zoomed, at, image);
		expect(after.x).toBeCloseTo(before.x, 9);
		expect(after.y).toBeCloseTo(before.y, 9);
	});
});

describe("panBy", () => {
	it("moves and stops at the edge", () => {
		const view = { k: 1, x: -100, y: -100 };
		expect(panBy(view, 30, -20, stage, image)).toEqual({ k: 1, x: -70, y: -120 });
		expect(panBy(view, 500, 500, stage, image)).toEqual({ k: 1, x: 0, y: 0 });
	});
});

describe("pinchView", () => {
	it("scales by the change of distance and follows the midpoint", () => {
		const start = { k: 1, x: -1000, y: -1000 };
		const a0 = { x: 300, y: 300 };
		const b0 = { x: 500, y: 300 };
		const a1 = { x: 200, y: 300 };
		const b1 = { x: 600, y: 300 };
		const view = pinchView(start, a0, b0, a1, b1, stage, image);
		expect(view.k).toBe(2);
		// The image point under the old midpoint is under the new one.
		const before = toFraction(start, { x: 400, y: 300 }, image);
		const after = toFraction(view, { x: 400, y: 300 }, image);
		expect(after.x).toBeCloseTo(before.x, 9);
		expect(after.y).toBeCloseTo(before.y, 9);
	});
});

describe("centerOn and the fraction maps", () => {
	it("puts a fraction in the middle of the stage", () => {
		const view = centerOn({ x: 0.5, y: 0.5 }, 1, stage, image);
		expect(toStage(view, { x: 0.5, y: 0.5 }, image)).toEqual({ x: 400, y: 300 });
	});

	it("round-trips", () => {
		const view = { k: 2, x: -1200, y: -800 };
		const fraction = toFraction(view, { x: 250, y: 125 }, image);
		const back = toStage(view, fraction, image);
		expect(back.x).toBeCloseTo(250, 9);
		expect(back.y).toBeCloseTo(125, 9);
	});
});
