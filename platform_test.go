// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

// purego older than v0.11.2 starts threads with signals unblocked on macOS
// without cgo, which crashes the runtime when a signal arrives during thread
// start (ebitengine/purego#553).
func TestPuregoHasSignalMaskFix(t *testing.T) {
	gomod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Skip("go.mod not available")
	}

	m := regexp.MustCompile(`github\.com/ebitengine/purego v0\.(\d+)\.(\d+)`).FindSubmatch(gomod)
	if m == nil {
		t.Fatal("purego requirement not found")
	}
	minor, _ := strconv.Atoi(string(m[1]))
	patch, _ := strconv.Atoi(string(m[2]))
	if minor < 11 || (minor == 11 && patch < 2) {
		t.Fatalf("purego v0.%d.%d lacks the signal mask fix, need v0.11.2 or later", minor, patch)
	}
}

// iOS matches the darwin build constraints but purego can't be built there
// without cgo, so importers of this package only build if iOS has its own
// backend.
func TestBuildsForIOSWithoutCgo(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available")
	}

	cmd := exec.Command(gobin, "build", ".")
	cmd.Env = append(os.Environ(), "GOOS=ios", "GOARCH=arm64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}
