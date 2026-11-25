// Where an upload's blueprint comes from: a tab of the upload form. Both tabs
// (pasted string, imported link) are sources with the same shape, so the form
// submits and checks whichever is active the same way.
import type { BlueprintInspection } from "@lib/api";

/**
 * What checking a source's blueprint came to: empty (nothing yet), checking
 * (the inspect call is out), valid (the API parsed it), invalid (it refused
 * it) or unknown (the check itself failed; the upload then judges it).
 */
export type CheckState = "empty" | "checking" | "valid" | "invalid" | "unknown";

/** Extra fields of the upload request a source contributes (an import names its source). */
export interface UploadFields {
	source_url?: string;
	source_token?: string;
	import_consent?: true;
}

export interface UploadSource {
	/** The exchange string this source would upload, as it is. */
	exchange(): string;
	state(): CheckState;
	/** The inspection of exchange() once valid. */
	inspection(): BlueprintInspection | undefined;
	/** Whether the upload button may be pressed with this source active. */
	submittable(): boolean;
	/** Fields to add to the upload request. */
	upload(): UploadFields;
	/** Calls `listener` whenever state, inspection or submittable() may have changed. */
	onChange(listener: () => void): void;
}
