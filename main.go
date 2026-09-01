package main

import (
	"embed"

	desktopapp "github.com/josexy/flowlens/backend/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

//go:embed build/trayicon-template.png
var trayTemplateIcon []byte

func main() {
	desktopapp.Run(desktopapp.Assets{
		Frontend:         assets,
		AppIcon:          icon,
		TrayTemplateIcon: trayTemplateIcon,
	})
}
