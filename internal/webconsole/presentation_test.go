package webconsole

import "testing"

func TestOperationMessageRefLocalizesUndoOperation(t *testing.T) {
	ref := operationMessageRef("undo activate")
	if ref == nil {
		t.Fatal("operationMessageRef(undo activate) = nil, want undo message ref")
	}
	if ref.Key != "plan.operation.undo_action" {
		t.Fatalf("ref.Key = %q, want plan.operation.undo_action", ref.Key)
	}
	if got := ref.Args["operation"]; got != "plan.operation.activate" {
		t.Fatalf("ref.Args[operation] = %q, want plan.operation.activate", got)
	}
}
