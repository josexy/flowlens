package settingservice

import (
	"maps"
	"slices"
)

// Clone returns a deep copy: the copy shares no pointer, slice, or map with
// s, and nil sections, slices, and maps stay nil. A disabled shortcut keeps
// its nil Binding.
func (s *Settings) Clone() *Settings {
	if s == nil {
		return nil
	}
	return &Settings{
		CommonConfig:             s.CommonConfig.Clone(),
		ProxyConfig:              s.ProxyConfig.Clone(),
		WindowConfig:             s.WindowConfig.Clone(),
		CacheConfig:              s.CacheConfig.Clone(),
		HistoryRetentionConfig:   s.HistoryRetentionConfig.Clone(),
		ProcessAttributionConfig: s.ProcessAttributionConfig.Clone(),
		TrafficTableConfig:       s.TrafficTableConfig.Clone(),
		TrafficDetailConfig:      s.TrafficDetailConfig.Clone(),
		PythonPluginConfig:       s.PythonPluginConfig.Clone(),
		Shortcuts:                s.Shortcuts.Clone(),
	}
}

// clonePlain copies a section that holds only values.
func clonePlain[T any](section *T) *T {
	if section == nil {
		return nil
	}
	clone := *section
	return &clone
}

// Clone returns a copy of the section.
func (c *CommonConfig) Clone() *CommonConfig { return clonePlain(c) }

// Clone returns a deep copy of the section.
func (c *ProxyConfig) Clone() *ProxyConfig {
	if c == nil {
		return nil
	}
	clone := *c
	clone.IncludeHosts = slices.Clone(c.IncludeHosts)
	clone.ExcludeHosts = slices.Clone(c.ExcludeHosts)
	clone.RootCAPaths = slices.Clone(c.RootCAPaths)
	clone.ClientCerts = slices.Clone(c.ClientCerts)
	return &clone
}

// Clone returns a copy of the section.
func (c *WindowConfig) Clone() *WindowConfig { return clonePlain(c) }

// Clone returns a copy of the section.
func (c *CacheConfig) Clone() *CacheConfig { return clonePlain(c) }

// Clone returns a copy of the section.
func (c *HistoryRetentionConfig) Clone() *HistoryRetentionConfig { return clonePlain(c) }

// Clone returns a copy of the section.
func (c *ProcessAttributionConfig) Clone() *ProcessAttributionConfig { return clonePlain(c) }

// Clone returns a deep copy of the section.
func (c *TrafficTableConfig) Clone() *TrafficTableConfig {
	if c == nil {
		return nil
	}
	return &TrafficTableConfig{
		HiddenColumns: slices.Clone(c.HiddenColumns),
		ColumnOrder:   slices.Clone(c.ColumnOrder),
		ColumnWidths:  maps.Clone(c.ColumnWidths),
	}
}

// Clone returns a deep copy of the section.
func (c *TrafficDetailConfig) Clone() *TrafficDetailConfig {
	if c == nil {
		return nil
	}
	return &TrafficDetailConfig{Layout: c.Layout, Splits: maps.Clone(c.Splits)}
}

// Clone returns a copy of the section.
func (c *PythonPluginConfig) Clone() *PythonPluginConfig { return clonePlain(c) }

// Clone returns a deep copy of the section, keeping disabled shortcuts.
func (c *ShortcutConfig) Clone() *ShortcutConfig {
	if c == nil {
		return nil
	}
	clone := &ShortcutConfig{}
	if c.Overrides != nil {
		clone.Overrides = make(map[string]ShortcutOverride, len(c.Overrides))
		for commandID, override := range c.Overrides {
			clone.Overrides[commandID] = override.Clone()
		}
	}
	return clone
}

// Clone returns a deep copy. A nil Binding, a disabled shortcut, stays nil.
func (o ShortcutOverride) Clone() ShortcutOverride {
	if o.Binding != nil {
		binding := *o.Binding
		binding.Modifiers = slices.Clone(o.Binding.Modifiers)
		o.Binding = &binding
	}
	return o
}
