# Build Directory

The build directory is used to house all the build files and assets for your application.

The structure is:

* bin - Output directory
* darwin - macOS specific files
* windows - Windows specific files

## Linux

`wails3 build` produces a standalone ELF binary in `bin/`. Linux ELF files do not embed an application icon. GTK4 desktop environments resolve the icon from the installed `.desktop` entry and icon theme using the application ID.

Use `wails3 package` and run the generated AppImage, or install the generated DEB/RPM/Arch package, when validating the launcher, Dock, or taskbar icon. The Linux desktop entry basename and icon name must remain aligned with the runtime application ID `org.wails.flowlens`.

The DEB also installs an AppArmor profile for `/usr/local/bin/flowlens`. Ubuntu 24.04 restricts unprivileged user namespaces by default, while WebKitGTK requires one for its bubblewrap sandbox. The profile grants that permission to FlowLens without disabling either the WebKit sandbox or the system-wide restriction.

## Mac

The `darwin` directory holds files specific to Mac builds.
These may be customised and used as part of the build. To return these files to the default state, simply delete them
and
build with `wails build`.

The directory contains the following files:

- `Info.plist` - the main plist file used for Mac builds. It is used when building using `wails build`.
- `Info.dev.plist` - same as the main plist file but used when building using `wails dev`.

## Windows

The `windows` directory contains the manifest and rc files used when building with `wails build`.
These may be customised for your application. To return these files to the default state, simply delete them and
build with `wails build`.

- `icon.ico` - The icon used for the application. This is used when building using `wails build`. If you wish to
  use a different icon, simply replace this file with your own. If it is missing, a new `icon.ico` file
  will be created using the `appicon.png` file in the build directory.
- `nsis/*` - The files used to create the Windows NSIS installer.
- `info.json` - Application details used for Windows builds. The data here will be used by the Windows installer,
  as well as the application itself (right click the exe -> properties -> details)
- `wails.exe.manifest` - The main application manifest file.
