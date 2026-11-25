package bridge

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLuaUploadTimeoutAndResponseContract(t *testing.T) {
	t.Parallel()
	lua, err := exec.LookPath("lua")
	if err != nil {
		t.Skip("Lua interpreter is not installed")
	}
	modPath, err := filepath.Abs("../../../../factorio/factorio-expo-bridge")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []string{"upload", "chunks", "analysis-force", "blueprint-position", "platform-analysis", "platform-hub", "analysis-golden", "result-writes", "client-disconnect", "unavailable-entities", "build-cost", "fluid-api", "train-rails"} {
		output, err := exec.Command(lua, filepath.Join(modPath, "tests/"+test+".lua"), modPath).CombinedOutput()
		if err != nil {
			t.Fatalf("bridge Lua %s regression tests failed: %v\n%s", test, err, output)
		}
	}
}
