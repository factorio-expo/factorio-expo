// Browser side of EntryDetailsFields: reads the details a form submitted
// (lib/entry-details.ts) and asks before a visibility change that deletes
// comments. Read before disabling the form: FormData skips disabled fields.
import { commentPurgeWarning, readEntryDetails, type EntryDetails } from "@lib/entry-details";

/** The form's entry details, or undefined when the viewer cancelled a comment purge. */
export function submittedEntryDetails(form: HTMLFormElement): EntryDetails | undefined {
	const details = readEntryDetails(new FormData(form));
	const fields = form.querySelector<HTMLElement>("[data-entry-details]");
	const warning = commentPurgeWarning(details.visibility, Number(fields?.dataset.commentCount));
	if (warning && !confirm(warning)) return undefined;
	return details;
}
