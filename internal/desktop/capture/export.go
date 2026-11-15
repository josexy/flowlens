package capture

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

const jobExport = "capture:export"

// ExportHost is what exporting needs from the application.
type ExportHost struct {
	Generation       func() uint64
	ExportLive       func(context.Context, proxyservice.TrafficExportRequest) (proxyservice.TrafficExportResult, error)
	ExportHistory    func(context.Context, historyservice.TrafficExportRequest) (proxyservice.TrafficExportResult, error)
	ExportLiveHAR    func(proxyservice.HARExportRequest) (proxyservice.HARWriteResult, error)
	ExportHistoryHAR func(historyservice.HARExportRequest) (proxyservice.HARWriteResult, error)
	// PickSavePath asks for a file; empty means canceled.
	PickSavePath func(filename, filterName, extension string) (string, error)
	// PickDirectory asks for a directory; empty means canceled.
	PickDirectory func(title string) (string, error)
}

// ExportOptions says what to export. Empty IDs means every entry.
type ExportOptions struct {
	HistoryKey   string
	IDs          []uint64
	Entry        *logic.TrafficEntry
	FilenameHint string
}

// Exporter writes traffic to files, one export at a time.
type Exporter struct {
	Host     ExportHost
	Runtime  store.Runtime
	Notifier *widgets.Notifier
	// Exporting is true while an export runs.
	Exporting bool
	now       func() time.Time
}

func uniqueIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]bool, len(ids))
	unique := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

func (e *Exporter) timeNow() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

type exportOutcome struct {
	result  proxyservice.TrafficExportResult
	har     proxyservice.HARWriteResult
	isHAR   bool
	stopped bool
}

// Export asks where to write and writes in the background.
func (e *Exporter) Export(format logic.TrafficExportFormat, options ExportOptions) bool {
	if e.Exporting || (options.IDs != nil && len(options.IDs) == 0) {
		return false
	}
	if options.IDs != nil {
		options.IDs = uniqueIDs(options.IDs)
	}
	e.Exporting = true
	runtime.Run(e.Runtime.Jobs, jobExport, func(ctx context.Context) (exportOutcome, error) {
		if format == logic.ExportHAR {
			return e.exportHAR(options)
		}
		return e.exportFiles(ctx, format, options)
	}, e.finish)
	return true
}

func (e *Exporter) finish(outcome exportOutcome, err error) {
	e.Exporting = false
	switch {
	case err != nil && outcome.isHAR:
		e.Notifier.Error(locales.HarExportFailed(err.Error()))
	case err != nil:
		e.Notifier.Error(locales.TrafficExportFailed(err.Error()))
	case outcome.stopped:
	case outcome.isHAR:
		message := locales.HarExportSuccess(strconv.Itoa(outcome.har.Exported), strconv.Itoa(outcome.har.MissingBodies), strconv.Itoa(outcome.har.Skipped))
		if outcome.har.Exported == 0 || outcome.har.Skipped > 0 || outcome.har.MissingBodies > 0 {
			e.Notifier.Warning(message)
			return
		}
		e.Notifier.Success(message)
	default:
		message := locales.TrafficExportResult(strconv.Itoa(outcome.result.Exported), strconv.Itoa(outcome.result.Skipped))
		if outcome.result.Exported == 0 || outcome.result.Skipped > 0 || outcome.result.HeadersDegraded {
			e.Notifier.Warning(message)
			return
		}
		e.Notifier.Success(message)
	}
}

func (e *Exporter) exportHAR(options ExportOptions) (exportOutcome, error) {
	outcome := exportOutcome{isHAR: true}
	var generation *uint64
	if options.HistoryKey == "" {
		value := e.Host.Generation()
		generation = &value
	}
	path, err := e.Host.PickSavePath(logic.MakeHARFilename(options.FilenameHint, e.timeNow()), locales.HarExportFileFilter(), ".har")
	if err != nil {
		return outcome, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		outcome.stopped = true
		return outcome, nil
	}
	path = logic.EnsureHARExtension(path)
	if options.HistoryKey != "" {
		outcome.har, err = e.Host.ExportHistoryHAR(historyservice.HARExportRequest{Key: options.HistoryKey, Path: path, TrafficIDs: options.IDs})
		return outcome, err
	}
	outcome.har, err = e.Host.ExportLiveHAR(proxyservice.HARExportRequest{Path: path, TrafficIDs: options.IDs, CaptureGeneration: generation})
	return outcome, err
}

func exportFormatLabel(format logic.TrafficExportFormat) string {
	switch format {
	case logic.ExportRequest:
		return locales.TrafficExportFormatsRequest()
	case logic.ExportRequestHeaders:
		return locales.TrafficExportFormatsRequestHeaders()
	case logic.ExportRequestBody:
		return locales.TrafficExportFormatsRequestBody()
	case logic.ExportResponse:
		return locales.TrafficExportFormatsResponse()
	case logic.ExportResponseHeaders:
		return locales.TrafficExportFormatsResponseHeaders()
	case logic.ExportResponseBody:
		return locales.TrafficExportFormatsResponseBody()
	case logic.ExportRequestResponse:
		return locales.TrafficExportFormatsRequestResponse()
	case logic.ExportHAR:
		return locales.TrafficExportFormatsHar()
	default:
		return locales.TrafficExportFormatsCsv()
	}
}

func (e *Exporter) exportFiles(ctx context.Context, format logic.TrafficExportFormat, options ExportOptions) (exportOutcome, error) {
	var outcome exportOutcome
	var generation *uint64
	if options.HistoryKey == "" {
		value := e.Host.Generation()
		generation = &value
	}
	directory := format != logic.ExportCSV && len(options.IDs) != 1
	extension := logic.TrafficExportExtension(format, options.Entry)
	var (
		path string
		err  error
	)
	if directory {
		path, err = e.Host.PickDirectory(locales.TrafficExportChooseDirectory())
	} else {
		base := strings.TrimSuffix(logic.MakeHARFilename(options.FilenameHint, e.timeNow()), ".har")
		path, err = e.Host.PickSavePath(base+"-"+string(format)+extension, exportFormatLabel(format), extension)
	}
	if err != nil {
		return outcome, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		outcome.stopped = true
		return outcome, nil
	}
	target := "file"
	if directory {
		target = "directory"
	} else {
		path = logic.EnsureFileExtension(path, extension)
	}
	kind := proxyservice.TrafficExportKind(format)
	if options.HistoryKey != "" {
		outcome.result, err = e.Host.ExportHistory(ctx, historyservice.TrafficExportRequest{Key: options.HistoryKey, Kind: kind, Path: path, TargetType: target, TrafficIDs: options.IDs})
		return outcome, err
	}
	outcome.result, err = e.Host.ExportLive(ctx, proxyservice.TrafficExportRequest{Kind: kind, Path: path, TargetType: target, TrafficIDs: options.IDs, CaptureGeneration: generation})
	return outcome, err
}
