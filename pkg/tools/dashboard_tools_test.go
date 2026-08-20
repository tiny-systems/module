package tools

import (
	"context"
	"strings"
	"testing"
)

// fakeDashboard records what the tools asked for. The interesting behaviour is
// in the asking — a host implements the writing.
type fakeDashboard struct {
	labelled   map[string]bool
	placements []WidgetPlacement
	pages      []DashboardPageInfo
	deleted    []string
	created    []string
}

func newFakeDashboard() *fakeDashboard {
	return &fakeDashboard{labelled: map[string]bool{}}
}

func (f *fakeDashboard) SetNodeWidget(_ context.Context, _, nodeID, port string, enabled bool) (string, error) {
	f.labelled[nodeID+":"+port] = enabled
	return "page", nil
}

func (f *fakeDashboard) ListPages(context.Context, string) ([]DashboardPageInfo, error) {
	return f.pages, nil
}

func (f *fakeDashboard) CreatePage(_ context.Context, _, title string) (DashboardPageInfo, error) {
	f.created = append(f.created, title)
	page := DashboardPageInfo{Name: "p-" + title, Title: title, SortIdx: len(f.pages)}
	f.pages = append(f.pages, page)
	return page, nil
}

func (f *fakeDashboard) DeletePage(_ context.Context, _, page string) error {
	f.deleted = append(f.deleted, page)
	return nil
}

func (f *fakeDashboard) PlaceWidget(_ context.Context, _ string, p WidgetPlacement) (DashboardPageInfo, error) {
	f.placements = append(f.placements, p)
	return DashboardPageInfo{Name: "p1", Title: "Dashboard", Widgets: []PlacedWidget{{NodeID: p.NodeID, Port: p.Port}}}, nil
}

func runTool(t *testing.T, tool interface {
	Execute(context.Context, ExecutionContext, map[string]interface{}) ToolResult
}, f *fakeDashboard, input map[string]interface{}) ToolResult {
	t.Helper()
	return tool.Execute(context.Background(), ExecutionContext{ProjectName: "proj", DashboardWriter: f}, input)
}

// Pinning has to do both halves: the label says the node is a widget, the
// placement says where it sits. One without the other is a widget with nowhere
// to be, which is how it ends up invisible on the dashboard it was added to.
func TestPinLabelsTheNodeAndPlacesIt(t *testing.T) {
	f := newFakeDashboard()
	res := runTool(t, NewSetNodeDashboardTool(), f, map[string]interface{}{
		"node_id": "flow.mod.signal-1",
		"page":    "Setup",
		"title":   "Start",
	})
	if !res.Success {
		t.Fatalf("pin failed: %s", res.Error)
	}
	if !f.labelled["flow.mod.signal-1:_control"] {
		t.Error("the node was never labelled as a widget")
	}
	if len(f.placements) != 1 {
		t.Fatalf("%d placements, want 1", len(f.placements))
	}
	p := f.placements[0]
	if p.Page != "Setup" || p.Title != "Start" {
		t.Errorf("placement = %+v, want page Setup titled Start", p)
	}
	if !p.AutoY {
		t.Error("no grid was given, so the widget must append below the page's content")
	}
}

func TestExplicitGridIsPassedThrough(t *testing.T) {
	f := newFakeDashboard()
	res := runTool(t, NewSetNodeDashboardTool(), f, map[string]interface{}{
		"node_id": "n1",
		"grid":    map[string]interface{}{"x": float64(3), "y": float64(2), "w": float64(3), "h": float64(4)},
	})
	if !res.Success {
		t.Fatalf("pin failed: %s", res.Error)
	}
	p := f.placements[0]
	if p.X != 3 || p.Y != 2 || p.W != 3 || p.H != 4 || p.AutoY {
		t.Fatalf("placement = %+v, want the grid honoured", p)
	}
}

// A grid with x and size but no row still means "append": choosing a column is
// not choosing a row, and defaulting y to 0 would drop the widget on top of
// whatever is already there.
func TestGridWithoutARowStillAppends(t *testing.T) {
	f := newFakeDashboard()
	runTool(t, NewSetNodeDashboardTool(), f, map[string]interface{}{
		"node_id": "n1",
		"grid":    map[string]interface{}{"x": float64(0), "w": float64(2)},
	})
	if !f.placements[0].AutoY {
		t.Fatal("a grid with no y must still append")
	}
}

// Unpinning takes the widget off every page. Leaving a placement behind means
// the next pin lands somewhere the caller never chose.
func TestUnpinRemovesFromEveryPage(t *testing.T) {
	f := newFakeDashboard()
	res := runTool(t, NewSetNodeDashboardTool(), f, map[string]interface{}{
		"node_id": "n1",
		"page":    "Setup",
		"enabled": false,
	})
	if !res.Success {
		t.Fatalf("unpin failed: %s", res.Error)
	}
	p := f.placements[0]
	if !p.Remove {
		t.Fatal("unpin did not remove the placement")
	}
	if p.Page != "" {
		t.Errorf("page = %q — unpinning clears the widget everywhere, not from one tab", p.Page)
	}
}

// The dashboard renders a node's control form and nothing else, so accepting
// another port would report a widget that can never appear.
func TestNonControlPortIsRefused(t *testing.T) {
	f := newFakeDashboard()
	res := runTool(t, NewSetNodeDashboardTool(), f, map[string]interface{}{
		"node_id": "n1",
		"port":    "_settings",
	})
	if res.Success {
		t.Fatal("a port the dashboard cannot render was accepted")
	}
	if !strings.Contains(res.Error, "_control") {
		t.Errorf("error does not say which port works: %s", res.Error)
	}
	if len(f.placements) != 0 {
		t.Error("a placement was stored for a port that never renders")
	}
}

func TestPageToolCreateRequiresATitle(t *testing.T) {
	f := newFakeDashboard()
	if res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "create"}); res.Success {
		t.Fatal("a page was created with no title — the tab would have no label")
	}
	res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "create", "title": "Setup"})
	if !res.Success {
		t.Fatalf("create failed: %s", res.Error)
	}
	if len(f.created) != 1 || f.created[0] != "Setup" {
		t.Fatalf("created = %v", f.created)
	}
}

func TestPageToolListAndDelete(t *testing.T) {
	f := newFakeDashboard()
	f.pages = []DashboardPageInfo{{Name: "p1", Title: "Setup"}}

	res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "list"})
	out, _ := res.Output.(map[string]interface{})
	if !res.Success || out["count"] != 1 {
		t.Fatalf("list = %+v", res)
	}

	if res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "delete"}); res.Success {
		t.Error("delete with no page was accepted")
	}
	if res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "delete", "page": "p1"}); !res.Success {
		t.Fatalf("delete failed: %s", res.Error)
	}
	if len(f.deleted) != 1 {
		t.Fatalf("deleted = %v", f.deleted)
	}
}

// An empty dashboard is the state an agent meets most often; it must say what
// to do rather than return an empty list and stop.
func TestEmptyPageListExplainsItself(t *testing.T) {
	f := newFakeDashboard()
	res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "list"})
	out, _ := res.Output.(map[string]interface{})
	if out["hint"] == nil {
		t.Fatal("no pages and no hint about how to get one")
	}
}

func TestUnknownActionIsRefused(t *testing.T) {
	f := newFakeDashboard()
	if res := runTool(t, NewDashboardPageTool(), f, map[string]interface{}{"action": "rename"}); res.Success {
		t.Fatal("an unsupported action was accepted")
	}
}
