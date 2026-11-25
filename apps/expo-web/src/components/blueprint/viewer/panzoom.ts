// The preview viewer's pan and zoom maths, free of the DOM. A view is where
// the image sits in the stage: `k` stage pixels per image pixel, and the
// stage position of the image's top-left corner. The image never leaves the
// stage: it is centred while it fits and clamped to the stage's edges once
// larger.

export interface Size {
	w: number;
	h: number;
}

export interface Point {
	x: number;
	y: number;
}

export interface View extends Point {
	k: number;
}

/** How far past the fitted size a visitor may zoom, in screen pixels per image pixel. */
const MAX_PIXEL_SCALE = 4;

/** The zoom range for an image in a stage: from fitted (never enlarged past
 * its own pixels) to at least `MAX_PIXEL_SCALE` screen pixels per image pixel. */
export function zoomLimits(stage: Size, image: Size): { min: number; max: number } {
	const fit = Math.min(stage.w / image.w, stage.h / image.h);
	const min = Math.min(fit, 1);
	return { min, max: Math.max(MAX_PIXEL_SCALE, min) };
}

/** Keeps the image inside the stage: centred on an axis where it is smaller. */
export function clampView(view: View, stage: Size, image: Size): View {
	const { min, max } = zoomLimits(stage, image);
	const k = Math.min(Math.max(view.k, min), max);
	const axis = (offset: number, stageSize: number, imageSize: number) => {
		const scaled = imageSize * k;
		if (scaled <= stageSize) return (stageSize - scaled) / 2;
		return Math.min(0, Math.max(stageSize - scaled, offset));
	};
	return { k, x: axis(view.x, stage.w, image.w), y: axis(view.y, stage.h, image.h) };
}

/** The whole image, centred. */
export function fitView(stage: Size, image: Size): View {
	return clampView({ k: 0, x: 0, y: 0 }, stage, image);
}

/** Zooms by `factor` keeping the image point under `at` (stage pixels) fixed. */
export function zoomAt(view: View, factor: number, at: Point, stage: Size, image: Size): View {
	const k = view.k * factor;
	const ratio = k / view.k;
	return clampView(
		{ k, x: at.x - (at.x - view.x) * ratio, y: at.y - (at.y - view.y) * ratio },
		stage,
		image,
	);
}

/** Moves the image by a stage-pixel offset. */
export function panBy(view: View, dx: number, dy: number, stage: Size, image: Size): View {
	return clampView({ ...view, x: view.x + dx, y: view.y + dy }, stage, image);
}

/**
 * A two-finger gesture: the view the fingers' starting positions `a0`, `b0`
 * (on `start`) have moved to as `a1`, `b1`: scaled by the change of distance
 * and moved with the midpoint.
 */
export function pinchView(
	start: View,
	a0: Point,
	b0: Point,
	a1: Point,
	b1: Point,
	stage: Size,
	image: Size,
): View {
	const before = Math.hypot(b0.x - a0.x, b0.y - a0.y) || 1;
	const after = Math.hypot(b1.x - a1.x, b1.y - a1.y);
	const k = start.k * (after / before);
	const mid0 = { x: (a0.x + b0.x) / 2, y: (a0.y + b0.y) / 2 };
	const mid1 = { x: (a1.x + b1.x) / 2, y: (a1.y + b1.y) / 2 };
	// The image point under the starting midpoint ends under the new one.
	const ratio = k / start.k;
	return clampView(
		{ k, x: mid1.x - (mid0.x - start.x) * ratio, y: mid1.y - (mid0.y - start.y) * ratio },
		stage,
		image,
	);
}

/** The view that centres an image fraction (0..1 on each axis) at zoom `k`. */
export function centerOn(fraction: Point, k: number, stage: Size, image: Size): View {
	return clampView(
		{ k, x: stage.w / 2 - fraction.x * image.w * k, y: stage.h / 2 - fraction.y * image.h * k },
		stage,
		image,
	);
}

/** Where a stage point lies on the image, as fractions: outside 0..1 means off the image. */
export function toFraction(view: View, at: Point, image: Size): Point {
	return { x: (at.x - view.x) / (image.w * view.k), y: (at.y - view.y) / (image.h * view.k) };
}

/** Where an image fraction is on the stage, in stage pixels. */
export function toStage(view: View, fraction: Point, image: Size): Point {
	return { x: view.x + fraction.x * image.w * view.k, y: view.y + fraction.y * image.h * view.k };
}
