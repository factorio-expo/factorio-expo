package blueprint

import "testing"

func toExport(node Node) ExportNode {
	if node.Kind == KindBlueprint {
		return ExportNode{Kind: KindBlueprint, ExchangeString: node.Blueprint.ExchangeString}
	}
	out := ExportNode{
		Kind: KindBook, Label: node.Book.Label, Description: node.Book.Description,
		Icons: node.Book.Icons, ActiveIndex: node.Book.ActiveIndex,
	}
	for _, child := range node.Book.Children {
		out.Children = append(out.Children, toExport(child))
	}
	return out
}

func shape(node Node) []string {
	if node.Kind == KindBlueprint {
		return []string{"bp:" + node.Blueprint.Label + node.Blueprint.Icons}
	}
	out := []string{"book:" + node.Book.Label + node.Book.Icons}
	for _, child := range node.Book.Children {
		out = append(out, shape(child)...)
	}
	return out
}

func TestEncodeTreeRoundTripsBook(t *testing.T) {
	t.Parallel()
	original, err := Parse(readFixture(t, "book-starter-kit.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	exported, err := EncodeTree(toExport(original), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(exported, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	want, got := shape(original), shape(again)
	if len(want) != len(got) {
		t.Fatalf("shape = %v, want %v", got, want)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("node %d = %q, want %q", i, got[i], want[i])
		}
	}
}
