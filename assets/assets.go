// Package assets embeds the application logo and Phosphor icon SVGs directly into the binary.
package assets

import (
	"embed"
)

//go:embed icons/*.svg
var Icons embed.FS

//go:embed app/logo-192.png
var AppLogoPNG []byte

// Icon asset paths within the embedded filesystem.
const (
	IconArrowUpBold    = "icons/arrow-up-bold.svg"
	IconArrowDownBold  = "icons/arrow-down-bold.svg"
	IconGaugeBold      = "icons/gauge-bold.svg"
	IconSunBold        = "icons/sun-bold.svg"
	IconMoonBold       = "icons/moon-bold.svg"
	IconMonitorBold    = "icons/monitor-bold.svg"
	IconGitHubLogoBold = "icons/github-logo-bold.svg"
	IconHeartBold      = "icons/heart-bold.svg"
	IconHeartFill      = "icons/heart-fill.svg"
)
