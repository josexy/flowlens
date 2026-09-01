#!/bin/sh

# Ubuntu 24.04 restricts unprivileged user namespaces by default. Load the
# application-specific permission required by WebKitGTK's bubblewrap sandbox.
flowlens_apparmor_profile=/etc/apparmor.d/usr.local.bin.flowlens
if [ -f "$flowlens_apparmor_profile" ] && command -v apparmor_parser >/dev/null 2>&1 && [ -d /sys/module/apparmor ]; then
  echo "Loading FlowLens AppArmor profile..."
  if ! apparmor_parser -r "$flowlens_apparmor_profile"; then
    echo "Warning: unable to load the FlowLens AppArmor profile. WebKitGTK may fail to start its sandbox." >&2
  fi
fi

# Update desktop database for .desktop file changes
# This makes the application appear in application menus and registers its capabilities.
if command -v update-desktop-database >/dev/null 2>&1; then
  echo "Updating desktop database..."
  update-desktop-database -q /usr/share/applications
else
  echo "Warning: update-desktop-database command not found. Desktop file may not be immediately recognized." >&2
fi

# GTK4 resolves the window icon by application ID through the active icon theme.
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  echo "Updating icon cache..."
  gtk-update-icon-cache -q -t /usr/share/icons/hicolor
else
  echo "Warning: gtk-update-icon-cache command not found. Application icon may not be immediately recognized." >&2
fi

# Update MIME database for custom URL schemes (x-scheme-handler)
# This ensures the system knows how to handle your custom protocols.
if command -v update-mime-database >/dev/null 2>&1; then
  echo "Updating MIME database..."
  update-mime-database -n /usr/share/mime
else
  echo "Warning: update-mime-database command not found. Custom URL schemes may not be immediately recognized." >&2
fi

exit 0
