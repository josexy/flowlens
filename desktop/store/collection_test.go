package store

import (
	"reflect"
	"testing"
)

func TestCollectionAllRowsIncludesCollapsedNodesInExistingOrder(t *testing.T) {
	s := &CollectionStore{Expanded: map[string]bool{}}
	s.setTree(&CollectionTree{Folders: []*CollectionFolder{
		{ID: "later", Name: "A", SortOrder: 2},
		{ID: "parent", Name: "Z", SortOrder: 1,
			Folders: []*CollectionFolder{
				nil,
				{ID: "beta", Name: "beta", SortOrder: 1},
				{ID: "alpha", Name: "Alpha", SortOrder: 1, Requests: []*CollectionRequest{{ID: "deep", Name: "Nested"}}},
			},
			Requests: []*CollectionRequest{nil, {ID: "request", Name: "Request"}},
		},
	}})
	all := s.AllRows()
	var ids []string
	for _, row := range all {
		ids = append(ids, row.ID)
	}
	if want := []string{"parent", "alpha", "deep", "beta", "request", "later"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("all rows = %v, want %v", ids, want)
	}
	if all[0].Children != 3 || all[1].ParentID != "parent" || all[2].Depth != 2 || all[2].ParentID != "alpha" || all[2].Request == nil {
		t.Fatalf("lost hierarchy or direct child count: %+v", all)
	}
	if len(s.Rows()) != 2 || len(s.Expanded) != 0 {
		t.Fatal("reading all nodes changed visible rows or expansion")
	}
	s.ToggleAllExpanded()
	if !reflect.DeepEqual(s.Rows(), s.AllRows()) {
		t.Fatal("expanded rows and full tree use different order or metadata")
	}
}
