// Every date and time on the site goes through Temporal. Where it is missing
// (older browsers; Node 26+ has it natively) temporal-polyfill is imported on
// demand; this module waits for it, so everything below is synchronous. Pages
// that only need it to upgrade times import it from a script of their own,
// so nothing else waits.
//
// Dates are shown ISO-style (2026-10-20). The server cannot know the viewer's
// time zone, so it renders UTC; in the browser `upgradeTimes` replaces those
// with the viewer's local time.
import type { Temporal as TemporalNamespace } from "temporal-polyfill";

type TemporalApi = typeof TemporalNamespace;
export type Instant = TemporalNamespace.Instant;

export const Temporal: TemporalApi =
	(globalThis as { Temporal?: TemporalApi }).Temporal ??
	(await import("temporal-polyfill")).Temporal;

/** An API timestamp (RFC 3339) as an Instant. */
export const instant = (value: string | Instant): Instant =>
	typeof value === "string" ? Temporal.Instant.from(value) : value;

function parts(value: string | Instant, zone: string) {
	const zoned = instant(value).toZonedDateTimeISO(zone);
	return [
		zoned.toPlainDate().toString(),
		zoned.toPlainTime().toString({ smallestUnit: "minute" }),
	] as const;
}

/** Whether `value` is a real calendar date written `2026-10-20`: what `<input type="date">` submits. */
export function isIsoDate(value: string): boolean {
	if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
	try {
		Temporal.PlainDate.from(value, { overflow: "reject" });
		return true;
	} catch {
		return false;
	}
}

const viewerZone = () => Temporal.Now.timeZoneId();

/** `2026-10-20` (UTC) */
export const isoDate = (value: string | Instant) => parts(value, "UTC")[0];

/** `2026-10-20 14:03 UTC` */
export function isoDateTime(value: string | Instant) {
	const [date, time] = parts(value, "UTC");
	return `${date} ${time} UTC`;
}

/** `2026-10-20` in `zone` (the viewer's by default). */
export const localIsoDate = (value: string | Instant, zone = viewerZone()) => parts(value, zone)[0];

/** `2026-10-20 16:03` in `zone` (the viewer's by default). */
export function localIsoDateTime(value: string | Instant, zone = viewerZone()) {
	const [date, time] = parts(value, zone);
	return `${date} ${time}`;
}

const durationFormat = new Intl.DurationFormat("en", { style: "short" });

/**
 * Time left until `until`, English: "6 days, 23 hr" (days drop the minutes),
 * "2 hr, 5 min", "12 min", "less than a minute"; undefined once past.
 */
export function remainingText(
	until: string | Instant,
	now: Instant = Temporal.Now.instant(),
): string | undefined {
	const end = instant(until);
	if (Temporal.Instant.compare(end, now) <= 0) return undefined;
	// Through UTC zoned times, so the difference may count days.
	const left = now.toZonedDateTimeISO("UTC").until(end.toZonedDateTimeISO("UTC"), {
		largestUnit: "days",
		smallestUnit: "minutes",
		roundingMode: "floor",
	});
	if (left.blank) return "less than a minute";
	const { days, hours, minutes } = left;
	return durationFormat.format(days > 0 ? { days, hours } : { hours, minutes });
}

/** "2026-11-20 16:03 (in 3 d 4 h)" in the viewer's zone, or "... (expired)". */
export function localIsoDateTimeWithRemaining(value: string | Instant, now?: Instant) {
	const left = remainingText(value, now);
	return `${localIsoDateTime(value)} ${left ? `(in ${left})` : "(expired)"}`;
}

/**
 * Browser only: swaps the server's UTC text in `<time data-local="date|datetime">`
 * for local time; `data-remaining` adds the time left until it.
 */
export function upgradeTimes(root: ParentNode = document) {
	for (const el of root.querySelectorAll<HTMLTimeElement>("time[data-local]")) {
		if (!el.dateTime) continue;
		try {
			if (el.dataset.local === "date") el.textContent = localIsoDate(el.dateTime);
			else if (el.dataset.remaining !== undefined)
				el.textContent = localIsoDateTimeWithRemaining(el.dateTime);
			else el.textContent = localIsoDateTime(el.dateTime);
		} catch {
			// Not a timestamp: keep the server's text.
		}
	}
}
