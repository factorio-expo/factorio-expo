/// <reference types="astro/client" />

declare namespace App {
	interface Locals {
		/** The signed-in staff member (their sign-in email); set by the middleware. */
		staff: { username: string };
	}
}
