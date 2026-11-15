package capture

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// BenchmarkTrafficTableLive measures a complete capture-pane update after one
// explicit batch flush. Setup, warm-up, database creation and cleanup are not
// timed. No sleeps are included: the 100 ms production batching interval can
// be used to convert B/op to allocation throughput at ten flushes per second.
func BenchmarkTrafficTableLive(b *testing.B) {
	for _, count := range []int{500, store.TrafficEntryCap} {
		for _, mode := range []string{"Idle", "Patch1", "SelectedPatch1", "Patch100", "Patch1000", "SortedPatch100", "Churn100", "Churn1000"} {
			churn := mode == "Churn100" || mode == "Churn1000"
			if (churn || mode == "Patch1000" || mode == "SelectedPatch1") && count != store.TrafficEntryCap {
				continue
			}
			b.Run(fmt.Sprintf("Rows%d/%s", count, mode), func(b *testing.B) {
				f := newFixtureSized(b, rangeOfEntries(count), 1400, 800)
				table := f.workspace.Table
				if mode == "SortedPatch100" {
					table.state.Sort = ui.SortOrder{Column: string(logic.ColumnStatusCode)}
					f.settle()
				}
				if mode == "SelectedPatch1" {
					table.replaceSelection(count - 1)
					f.settle()
				}
				if len(table.rows) != count {
					b.Fatalf("requested %d rows, actually showing %d", count, len(table.rows))
				}
				revision, nextID := uint64(2), uint64(count+1)
				step := func() {
					batch := 100
					switch mode {
					case "Patch1", "SelectedPatch1":
						batch = 1
					case "Patch1000", "Churn1000":
						batch = 1000
					}
					if mode != "Idle" {
						for index := range batch {
							if churn {
								f.data.Traffic.AddOrUpdate(entry(nextID, "new.test"))
								nextID++
								continue
							}
							current := f.data.Traffic.Entries[len(f.data.Traffic.Entries)-batch+index]
							updated := *current
							updated.Revision = revision
							updated.StatusCode = 200 + int((revision+uint64(index))%300)
							f.data.Traffic.AddOrUpdate(&updated)
						}
						f.data.Traffic.FlushPending()
						f.window.Drain()
						revision++
					}
					f.tester.Frame()
				}
				for range 3 {
					step()
				}
				table.CellBuilds = 0
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					step()
				}
				b.StopTimer()
				b.ReportMetric(float64(table.CellBuilds)/float64(b.N), "cells/op")
				if len(table.rows) != count {
					b.Fatalf("row count changed to %d", len(table.rows))
				}
				if churn && table.rows[len(table.rows)-1].ID != nextID-1 {
					b.Fatal("new requests were queued instead of displayed")
				}
				if mode != "Idle" && !churn && f.data.Traffic.Entries[count-1].Revision != revision-1 {
					b.Fatal("patches were not applied")
				}
			})
		}
	}
}

// benchmarkUncappedSource supplies historical-sized snapshots without silently
// truncating them to the live store's 5,000-row cap. Only filtering and the table
// surface participate; networking, disk I/O and the capture sidebar do not.
type benchmarkUncappedSource struct {
	Source
	filter *store.FilterStore
}

func (s benchmarkUncappedSource) Filter() *store.FilterStore { return s.filter }
func (s benchmarkUncappedSource) Live() LiveControl          { return nil }

func BenchmarkTrafficTableStress(b *testing.B) {
	for _, count := range []int{5000, 50000, 100000} {
		for _, changed := range []bool{false, true} {
			mode := "Idle"
			if changed {
				mode = "Patch1"
			}
			b.Run(fmt.Sprintf("Rows%d/%s", count, mode), func(b *testing.B) {
				f := newFixtureSized(b, rangeOfEntries(1), 1400, 800)
				entries, version := rangeOfEntries(count), uint64(1)
				filter := store.NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
				table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
				tester := ui.NewTester(func(c *ui.Context) {
					theme := nui.App(c, f.data.Settings.Theme().Config)
					table.Build(c, theme)
				}, 1160, 714)
				step := func() {
					if changed {
						version++
						updated := *entries[len(entries)-1]
						updated.Revision, updated.StatusCode = version, 200+int(version%300)
						entries[len(entries)-1] = &updated
					}
					tester.Frame()
				}
				for range 3 {
					step()
				}
				table.CellBuilds = 0
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					step()
				}
				b.StopTimer()
				b.ReportMetric(float64(table.CellBuilds)/float64(b.N), "cells/op")
				if len(table.rows) != count || table.rows[count-1].Revision != version {
					b.Fatal("stress table did not retain or update every row")
				}
			})
		}
	}
}

// BenchmarkTrafficSetRows separates Nuxt's record copy/ID validation from the
// fresh string IDs and index slice allocated by the migration's refreshRows.
func BenchmarkTrafficSetRows(b *testing.B) {
	for _, count := range []int{500, 5000, 50000, 100000} {
		for _, mode := range []string{"CachedIDs", "FormatIDs", "FullAdapter"} {
			b.Run(fmt.Sprintf("Rows%d/%s", count, mode), func(b *testing.B) {
				indices, ids := make([]int, count), make([]string, count)
				for index := range indices {
					indices[index], ids[index] = index, strconv.FormatUint(uint64(index+1), 10)
				}
				key := func(index int) string { return ids[index] }
				if mode != "CachedIDs" {
					key = func(index int) string { return strconv.FormatUint(uint64(index+1), 10) }
				}
				var state nui.TableState[int]
				state.SetRows(indices, key)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if mode == "FullAdapter" {
						indices = make([]int, count)
						for index := range indices {
							indices[index] = index
						}
					}
					state.SetRows(indices, key)
				}
				b.StopTimer()
				runtime.KeepAlive(&state)
			})
		}
	}
}
