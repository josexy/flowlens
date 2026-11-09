// Package resources embeds the files the application needs at run time.
package resources

import _ "embed"

// AppIcon is the application icon as a PNG.
//
//go:embed icon.png
var AppIcon []byte

// TrayTemplateIcon is the macOS menu bar icon as a PNG template.
//
//go:embed trayicon-template.png
var TrayTemplateIcon []byte

// DMSans is the bundled interface font.
//
//go:embed fonts/DMSans.ttf
var DMSans []byte

// JetBrainsMono is the bundled code font.
//
//go:embed fonts/JetBrainsMono.ttf
var JetBrainsMono []byte
