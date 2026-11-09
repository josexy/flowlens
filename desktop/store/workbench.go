package store

// WorkspaceSection is a page of the workbench.
type WorkspaceSection string

// Workbench sections, in sidebar order.
const (
	WorkspaceCapture       WorkspaceSection = "capture"
	WorkspaceCategory      WorkspaceSection = "category"
	WorkspaceAPICollection WorkspaceSection = "apiCollection"
	WorkspaceRewriteRules  WorkspaceSection = "rewriteRules"
	WorkspacePythonPlugins WorkspaceSection = "pythonPlugins"
	WorkspaceMemStats      WorkspaceSection = "memstats"
)

// WorkspaceSections lists the sections in sidebar order.
var WorkspaceSections = []WorkspaceSection{
	WorkspaceCapture, WorkspaceCategory, WorkspaceAPICollection, WorkspaceRewriteRules, WorkspacePythonPlugins, WorkspaceMemStats,
}

// ContentSurface is what fills the content area.
type ContentSurface string

// Content surfaces.
const (
	ContentTraffic       ContentSurface = "traffic"
	ContentRewriteRules  ContentSurface = "rewriteRules"
	ContentPythonPlugins ContentSurface = "pythonPlugins"
	ContentMemStats      ContentSurface = "memstats"
)

// CaptureItemLive selects the live capture in the Capture section.
const CaptureItemLive = "capture"

// WorkbenchStore holds which page the main window shows.
type WorkbenchStore struct {
	ActiveSection           WorkspaceSection
	ActiveContent           ContentSurface
	SecondarySidebarVisible bool
	// SecondarySidebarPercent is the sidebar's share of the content width, kept
	// while the sidebar is collapsed.
	SecondarySidebarPercent float32
	// CaptureSelection is the item chosen in the Capture section, retained
	// while other sections are shown.
	CaptureSelection string
	// PythonHelpScope is the API help scope on show, "" when closed.
	PythonHelpScope string
}

// NewWorkbenchStore returns the state of a new window.
func NewWorkbenchStore() *WorkbenchStore {
	return &WorkbenchStore{
		ActiveSection:           WorkspaceCapture,
		ActiveContent:           ContentTraffic,
		SecondarySidebarVisible: true,
		SecondarySidebarPercent: 20,
		CaptureSelection:        CaptureItemLive,
	}
}

func contentForSection(section WorkspaceSection) ContentSurface {
	switch section {
	case WorkspaceRewriteRules:
		return ContentRewriteRules
	case WorkspacePythonPlugins:
		return ContentPythonPlugins
	case WorkspaceMemStats:
		return ContentMemStats
	default:
		return ContentTraffic
	}
}

// ActivateSection opens a section. Activating the open section toggles its
// secondary sidebar.
func (w *WorkbenchStore) ActivateSection(section WorkspaceSection) {
	next := contentForSection(section)
	if w.ActiveSection == section {
		if w.ActiveContent != next {
			w.ActiveContent = next
			w.SecondarySidebarVisible = true
			return
		}
		w.SecondarySidebarVisible = !w.SecondarySidebarVisible
		return
	}
	w.ActiveSection = section
	w.SecondarySidebarVisible = true
	w.ActiveContent = next
}

// ShowSection opens a section and its sidebar, never toggling it.
func (w *WorkbenchStore) ShowSection(section WorkspaceSection) {
	w.ActiveSection = section
	w.ActiveContent = contentForSection(section)
	w.SecondarySidebarVisible = true
}

// SelectCaptureItem shows an item of the Capture section.
func (w *WorkbenchStore) SelectCaptureItem(item string) {
	w.ActiveSection = WorkspaceCapture
	w.SecondarySidebarVisible = true
	w.CaptureSelection = item
	w.ActiveContent = ContentTraffic
}

// OpenPythonHelp shows the API help for a scope.
func (w *WorkbenchStore) OpenPythonHelp(scope string) { w.PythonHelpScope = scope }

// ClosePythonHelp hides the API help.
func (w *WorkbenchStore) ClosePythonHelp() { w.PythonHelpScope = "" }
