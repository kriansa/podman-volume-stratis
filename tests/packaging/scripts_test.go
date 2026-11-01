// Package packaging tests the scripts the RPM runs when it is installed, upgraded and removed.
package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const fixScript = "/usr/libexec/fix-stratisd-ordering.sh"

// runScript runs one of the package's scripts the way rpm does, with sh and the given arguments,
// and returns the commands it ran. systemctl, rm and the stratisd fix script are replaced by
// stand-ins that only record their arguments, so nothing on this machine is touched.
func runScript(t *testing.T, script string, args ...string) []string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")

	for _, name := range []string{"systemctl", "rm", "fix-stratisd-ordering.sh"} {
		stub := "#!/bin/sh\necho \"" + name + " $*\" >> " + calls + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// The fix script is called by its full path, which the stand-ins cannot shadow, so the script
	// runs from a copy that calls the stand-in instead.
	source, err := os.ReadFile(filepath.Join(scriptsDir(), script))
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(dir, script)
	content := strings.ReplaceAll(string(source), fixScript, filepath.Join(dir, "fix-stratisd-ordering.sh"))
	if err := os.WriteFile(copied, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", append([]string{copied}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", script, args, err, output)
	}

	recorded, err := os.ReadFile(calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(recorded)), "\n")
}

// scriptsDir finds the package's scripts from this file's own location, so the tests do not depend
// on the directory they run from.
func scriptsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "build", "packaging", "scripts")
}

// rpm passes the removal script how many copies of the package will be left, which is 1 during an
// upgrade. Stopping the plugin then would stop every volume and container that requires it, with
// nothing to start them again, and removing the stratisd override would undo the new version's.
func TestPreremove_UpgradeChangesNothing(t *testing.T) {
	if calls := runScript(t, "preremove.sh", "1"); len(calls) != 0 {
		t.Errorf("during an upgrade, preremove.sh ran %q; want nothing", calls)
	}
}

var removal = []string{
	"systemctl stop podman-volume-stratis",
	"systemctl disable podman-volume-stratis",
	"systemctl stop stratisd-fix-ordering.path",
	"systemctl disable stratisd-fix-ordering.path",
	"rm -f /etc/systemd/system/stratisd.service",
	"systemctl daemon-reload",
}

func TestPreremove_RemovalUndoesEverything(t *testing.T) {
	if calls := runScript(t, "preremove.sh", "0"); !reflect.DeepEqual(calls, removal) {
		t.Errorf("on removal, preremove.sh ran %q; want %q", calls, removal)
	}
}

// Without an argument, such as when run by hand, it is treated as a removal, as it always was.
func TestPreremove_NoArgumentIsARemoval(t *testing.T) {
	if calls := runScript(t, "preremove.sh"); !reflect.DeepEqual(calls, removal) {
		t.Errorf("without an argument, preremove.sh ran %q; want %q", calls, removal)
	}
}

// posttrans runs after every install and upgrade, after the old version's removal script, which up
// to 1.1.4 undid the stratisd override and the path watcher even during an upgrade. It sets them
// up again, then restarts the plugin last, without waiting, and only if it is already running.
func TestPosttrans_RestoresTheOverrideThenRestartsThePlugin(t *testing.T) {
	want := []string{
		"fix-stratisd-ordering.sh ",
		"systemctl enable --now stratisd-fix-ordering.path",
		"systemctl daemon-reload",
		"systemctl try-restart --no-block podman-volume-stratis",
	}
	if calls := runScript(t, "posttrans.sh"); !reflect.DeepEqual(calls, want) {
		t.Errorf("posttrans.sh ran %q; want %q", calls, want)
	}
}
