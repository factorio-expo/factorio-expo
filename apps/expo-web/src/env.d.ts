declare namespace App {
	interface Locals {
		/** Who is viewing, resolved once per request (lib/viewer.ts `getViewer`). */
		viewer?: Promise<import("./lib/viewer").Viewer>;
	}
}
