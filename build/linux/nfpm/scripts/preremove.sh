#!/bin/sh

flowlens_apparmor_profile=/etc/apparmor.d/usr.local.bin.flowlens
if [ -f "$flowlens_apparmor_profile" ] && command -v apparmor_parser >/dev/null 2>&1 && [ -d /sys/module/apparmor ]; then
  echo "Unloading FlowLens AppArmor profile..."
  if ! apparmor_parser -R "$flowlens_apparmor_profile"; then
    echo "Warning: unable to unload the FlowLens AppArmor profile." >&2
  fi
fi

exit 0
