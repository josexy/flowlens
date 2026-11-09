package store

import "testing"

func TestSwitchingSectionsRetainsTheCaptureSelection(t *testing.T) {
	w := NewWorkbenchStore()
	w.SelectCaptureItem("history-1")
	w.ActivateSection(WorkspaceRewriteRules)
	if w.ActiveContent != ContentRewriteRules || !w.SecondarySidebarVisible {
		t.Fatalf("workbench %+v", w)
	}
	w.ActivateSection(WorkspaceCapture)
	if w.CaptureSelection != "history-1" || w.ActiveContent != ContentTraffic {
		t.Fatalf("workbench %+v", w)
	}
}

func TestActivatingTheOpenSectionTogglesItsSidebar(t *testing.T) {
	w := NewWorkbenchStore()
	w.ActivateSection(WorkspaceCapture)
	if w.SecondarySidebarVisible {
		t.Fatal("sidebar should collapse")
	}
	w.ActivateSection(WorkspaceCapture)
	if !w.SecondarySidebarVisible {
		t.Fatal("sidebar should expand")
	}
}

func TestSectionsMapToContent(t *testing.T) {
	for section, want := range map[WorkspaceSection]ContentSurface{
		WorkspaceCapture: ContentTraffic, WorkspaceCategory: ContentTraffic, WorkspaceAPICollection: ContentTraffic,
		WorkspaceRewriteRules: ContentRewriteRules, WorkspacePythonPlugins: ContentPythonPlugins, WorkspaceMemStats: ContentMemStats,
	} {
		if got := contentForSection(section); got != want {
			t.Errorf("%s: got %s", section, got)
		}
	}
}
