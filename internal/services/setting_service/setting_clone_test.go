package settingservice

import (
	"fmt"
	"reflect"
	"testing"
)

// fillValue sets every exported field reachable from value to a non-zero
// value derived from seed. Pointers are allocated; slices and maps get two
// elements each.
func fillValue(t *testing.T, value reflect.Value, seed *int) {
	t.Helper()
	*seed++
	switch value.Kind() {
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
		fillValue(t, value.Elem(), seed)
	case reflect.Struct:
		for index := range value.NumField() {
			if value.Type().Field(index).IsExported() {
				fillValue(t, value.Field(index), seed)
			}
		}
	case reflect.Slice:
		value.Set(reflect.MakeSlice(value.Type(), 2, 2))
		for index := range 2 {
			fillValue(t, value.Index(index), seed)
		}
	case reflect.Map:
		value.Set(reflect.MakeMap(value.Type()))
		for range 2 {
			key := reflect.New(value.Type().Key()).Elem()
			element := reflect.New(value.Type().Elem()).Elem()
			fillValue(t, key, seed)
			fillValue(t, element, seed)
			value.SetMapIndex(key, element)
		}
	case reflect.String:
		value.SetString(fmt.Sprintf("value-%d", *seed))
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(int64(*seed))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(uint64(*seed))
	case reflect.Float32, reflect.Float64:
		value.SetFloat(float64(*seed) + 0.5)
	default:
		t.Fatalf("cannot fill a %s; extend the test", value.Type())
	}
}

// mutateValue changes every value reachable from value, through pointers,
// slices, and maps, so that any storage shared with another value shows.
func mutateValue(t *testing.T, value reflect.Value) {
	t.Helper()
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			mutateValue(t, value.Elem())
		}
	case reflect.Struct:
		for index := range value.NumField() {
			if value.Type().Field(index).IsExported() {
				mutateValue(t, value.Field(index))
			}
		}
	case reflect.Slice:
		for index := range value.Len() {
			mutateValue(t, value.Index(index))
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			element := reflect.New(value.Type().Elem()).Elem()
			element.Set(value.MapIndex(key))
			mutateValue(t, element)
			value.SetMapIndex(key, element)
		}
		extra := reflect.New(value.Type().Key()).Elem()
		if extra.Kind() == reflect.String {
			extra.SetString("added-by-mutation")
			value.SetMapIndex(extra, reflect.New(value.Type().Elem()).Elem())
		}
	case reflect.String:
		value.SetString(value.String() + "-changed")
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(value.Int() + 1000)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(value.Uint() + 1000)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(value.Float() + 1000)
	default:
		t.Fatalf("cannot mutate a %s; extend the test", value.Type())
	}
}

// requireNoZeroField fails for an exported field the fill left zero, which
// would hide a field the clone forgets.
func requireNoZeroField(t *testing.T, value reflect.Value, path string) {
	t.Helper()
	if value.IsZero() {
		t.Fatalf("%s is zero after filling", path)
	}
	switch value.Kind() {
	case reflect.Pointer:
		requireNoZeroField(t, value.Elem(), path)
	case reflect.Struct:
		for index := range value.NumField() {
			if field := value.Type().Field(index); field.IsExported() {
				requireNoZeroField(t, value.Field(index), path+"."+field.Name)
			}
		}
	case reflect.Slice:
		for index := range value.Len() {
			requireNoZeroField(t, value.Index(index), fmt.Sprintf("%s[%d]", path, index))
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			requireNoZeroField(t, value.MapIndex(key), fmt.Sprintf("%s[%v]", path, key))
		}
	}
}

func filledSettings(t *testing.T) *Settings {
	t.Helper()
	settings := new(Settings)
	seed := 0
	fillValue(t, reflect.ValueOf(settings).Elem(), &seed)
	return settings
}

func TestSettingsCloneCopiesEveryFieldWithoutSharing(t *testing.T) {
	original, pristine := filledSettings(t), filledSettings(t)
	requireNoZeroField(t, reflect.ValueOf(original).Elem(), "Settings")
	clone := original.Clone()
	if !reflect.DeepEqual(clone, original) {
		t.Fatalf("clone differs from the original:\n%+v\n%+v", clone, original)
	}
	mutateValue(t, reflect.ValueOf(clone).Elem())
	if reflect.DeepEqual(clone, pristine) {
		t.Fatal("mutating the clone changed nothing; the test does not reach its fields")
	}
	if !reflect.DeepEqual(original, pristine) {
		t.Fatal("mutating the clone changed the original: a pointer, slice, or map is shared")
	}
}

func TestSettingsCloneKeepsNilAndEmptyValues(t *testing.T) {
	if (*Settings)(nil).Clone() != nil {
		t.Fatal("a nil Settings cloned to a value")
	}
	settings := &Settings{
		ProxyConfig:        &ProxyConfig{IncludeHosts: []string{}},
		TrafficTableConfig: &TrafficTableConfig{ColumnWidths: map[string]float32{}},
		Shortcuts: &ShortcutConfig{Overrides: map[string]ShortcutOverride{
			"app.save": {Binding: nil, Scope: ShortcutScopeApplication},
		}},
	}
	clone := settings.Clone()
	if !reflect.DeepEqual(clone, settings) {
		t.Fatalf("clone = %+v", clone)
	}
	if clone.CommonConfig != nil || clone.ProxyConfig.ExcludeHosts != nil || clone.ProxyConfig.IncludeHosts == nil || clone.TrafficTableConfig.ColumnWidths == nil {
		t.Fatal("nil and empty values were not kept apart")
	}
	disabled, ok := clone.Shortcuts.Overrides["app.save"]
	if !ok || disabled.Binding != nil || disabled.Scope != ShortcutScopeApplication {
		t.Fatalf("disabled shortcut = %+v, %t", disabled, ok)
	}
}
