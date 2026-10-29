package desktop

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// appBrowsers are Chromium-based browsers, whose --app flag opens a page
// in a window of its own: no tabs, no address bar. What they are called on
// PATH, then where macOS keeps them.
var appBrowsers = []string{
	"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
	"microsoft-edge", "microsoft-edge-stable", "brave-browser", "brave",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
}

// lookPath is exec.LookPath, replaceable in tests.
var lookPath = exec.LookPath

// WindowCommand is how url opens in a window: a Chromium-based browser in
// app mode with a profile of mad's own (its windows don't mix with the
// user's browsing, and closing them is closing mad's), else the system's
// default browser.
func WindowCommand(url, profileDir string) *exec.Cmd {
	for _, b := range appBrowsers {
		path, err := lookPath(b)
		if err != nil {
			continue
		}
		return exec.Command(path, "--app="+url, "--user-data-dir="+profileDir,
			"--no-first-run", "--no-default-browser-check", "--window-size=1400,900")
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url)
	}
	return exec.Command("xdg-open", url)
}

// ProfileDir is where the app window's browser profile lives.
func ProfileDir(stateDir string) string {
	return filepath.Join(stateDir, "desktop-profile")
}
