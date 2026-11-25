package main

import (
	"fmt"
	"os"

	"factorio-expo/apps/expo-api/internal/httpapi"
)

// writeOpenAPI prints one listener's OpenAPI document: the public one
// (`expo-api openapi > apps/expo-api/openapi/public.json`) or, with
// `admin`, the staff listener's (`expo-api openapi admin >
// apps/expo-api/openapi/admin.json`). The frontends generate their API
// types from the checked-in files.
func writeOpenAPI(args []string) int {
	spec, err := httpapi.PublicSpec()
	if len(args) > 0 && args[0] == "admin" {
		spec, err = httpapi.AdminSpec()
	} else if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "openapi: unknown document", args[0], "(want none or admin)")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		return 1
	}
	if _, err := os.Stdout.Write(spec); err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		return 1
	}
	return 0
}
