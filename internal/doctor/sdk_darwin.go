package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// sdkHint explains a build that fails only because of the macOS SDK. When the Command
// Line Tools are newer than the selected Xcode, xcrun can give the compiler the newer
// tools' SDK, whose libraries the older linker can't read ("unknown architecture"), so
// every link fails. Building again with the selected tools' own SDK tells that apart
// from any other failure, and shows the setting that fixes it.
func sdkHint(ctx context.Context, getenv func(string) string, build func(env ...string) ([]byte, error)) []string {
	if getenv("SDKROOT") != "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, "xcrun", "--sdk", "macosx", "--show-sdk-path").Output()
	if err != nil {
		return nil
	}
	own := strings.TrimSpace(string(out))
	if _, err := build("SDKROOT=" + own); err != nil {
		return nil
	}
	return []string{
		"",
		"It builds with SDKROOT set to the SDK of the developer tools xcode-select chose,",
		fmt.Sprintf("%s, so the default SDK doesn't suit those tools.", own),
		"To fix it, add this line to your shell's startup file:",
		`    export SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"`,
		"or update Xcode, so that its tools can read the default SDK.",
	}
}
