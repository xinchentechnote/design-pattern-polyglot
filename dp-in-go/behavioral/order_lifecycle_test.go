package behavioral

import (
	"errors"
	"strings"
	"testing"
)

// 与 Rust 版 order_lifecycle.rs / Java 版 OrderLifecycleTest / C++ 版 order_lifecycle_test
// 的 12 例测试断言一一对应。

// failureOf 执行 fn 并把返回的 error 断言为 *OrderError（对应 Java 的 assertThrows）。
func failureOf(t *testing.T, fn func() error) *OrderError {
	t.Helper()
	err := fn()
	var oe *OrderError
	if errors.As(err, &oe) {
		return oe
	}
	t.Fatalf("expected *OrderError, got %v", err)
	return nil
}

func TestSubmitTransitionsCreatedToSubmitted(t *testing.T) {
	order := NewManagedOrder(1, SideBuy, 100, 10)
	if order.State() != StateCreated {
		t.Fatalf("initial state = %s, want Created", order.State())
	}
	if err := order.Submit(); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if order.State() != StateSubmitted {
		t.Fatalf("state after submit = %s, want Submitted", order.State())
	}
	// 已提交的单重复提交：非法转换显式拒绝
	err := failureOf(t, order.Submit)
	if err.Reason != ReasonIllegalTransition || err.Action != "submit" || err.From != StateSubmitted {
		t.Fatalf("duplicate submit err = %+v, want illegal transition submit from Submitted", err)
	}
}

func TestRejectFromCreatedState(t *testing.T) {
	order := NewManagedOrder(1, SideBuy, 100, 10)
	if err := order.Reject(); err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if !order.State().IsTerminal() {
		t.Fatalf("state after reject = %s, want terminal", order.State())
	}
	// 终态不可再撤/改/成交
	if order.State().CanCancel() || order.State().CanModify() || order.State().CanFill() {
		t.Fatalf("terminal state %s must not allow cancel/modify/fill", order.State())
	}
}

func TestFillProgressesToPartiallyFilledThenFilled(t *testing.T) {
	order := NewManagedOrder(1, SideSell, 101, 10)
	if err := order.Submit(); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if err := order.Fill(3); err != nil {
		t.Fatalf("fill 3 failed: %v", err)
	}
	if order.State() != StatePartiallyFilled || order.RemainingQuantity() != 7 {
		t.Fatalf("after fill 3: state=%s remaining=%d, want PartiallyFilled/7",
			order.State(), order.RemainingQuantity())
	}
	if err := order.Fill(7); err != nil {
		t.Fatalf("fill 7 failed: %v", err)
	}
	if order.State() != StateFilled {
		t.Fatalf("state after fill 7 = %s, want Filled", order.State())
	}
	// 超量成交与终态后成交都显式拒绝
	if err := failureOf(t, func() error { return order.Fill(1) }); err.Reason != ReasonIllegalTransition ||
		err.From != StateFilled {
		t.Fatalf("fill after Filled err = %+v, want illegal transition fill from Filled", err)
	}

	order2 := NewManagedOrder(2, SideSell, 101, 5)
	if err := order2.Submit(); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	err := failureOf(t, func() error { return order2.Fill(6) })
	if err.Reason != ReasonOverfill || err.Request != 6 || err.Have != 5 {
		t.Fatalf("overfill err = %+v, want requested 6 remaining 5", err)
	}
}

func TestCancelFilledOrderRejected(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Fill(id, 10); err != nil {
		t.Fatalf("fill failed: %v", err)
	}

	// 已成交不可撤：显式拒绝，订单保持 Filled
	err := failureOf(t, func() error { return oms.Execute(NewCancelCommand(id)) })
	if err.Reason != ReasonIllegalTransition || err.Action != "cancel" || err.From != StateFilled {
		t.Fatalf("cancel filled err = %+v, want illegal transition cancel from Filled", err)
	}
	if oms.Order(id).State() != StateFilled {
		t.Fatalf("state = %s, want Filled", oms.Order(id).State())
	}
	// 失败的命令不进入历史
	if oms.HistoryLen() != 0 {
		t.Fatalf("history len = %d, want 0", oms.HistoryLen())
	}
	if err := oms.UndoLast(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("undoLast err = %v, want ErrNothingToUndo", err)
	}
	log := oms.AuditLog()
	if !strings.Contains(log[len(log)-1], "UNDO -> FAILED: nothing to undo") {
		t.Fatalf("last audit = %q, want empty undo failure", log[len(log)-1])
	}
}

func TestCancelCommandMovesToCancelled(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	view := oms.Order(id)
	if err := view.Modify(999, 10); err != nil {
		t.Fatalf("modify query copy failed: %v", err)
	}
	if oms.Order(id).Price() != 100 {
		t.Fatalf("query view mutated internal order: price=%d, want 100", oms.Order(id).Price())
	}
	if err := oms.Execute(NewCancelCommand(id)); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if oms.Order(id).State() != StateCancelled {
		t.Fatalf("state = %s, want Cancelled", oms.Order(id).State())
	}
	if oms.HistoryLen() != 1 {
		t.Fatalf("history len = %d, want 1", oms.HistoryLen())
	}
}

func TestUndoCancelRestoresStateAndFills(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Fill(id, 2); err != nil { // 部分成交后撤单
		t.Fatalf("fill failed: %v", err)
	}
	if err := oms.Execute(NewCancelCommand(id)); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if oms.Order(id).State() != StateCancelled {
		t.Fatalf("state = %s, want Cancelled", oms.Order(id).State())
	}

	if err := oms.UndoLast(); err != nil {
		t.Fatalf("undoLast failed: %v", err)
	}
	order := oms.Order(id)
	// 快照回滚：不只是状态，成交进度一并恢复
	if order.State() != StatePartiallyFilled || order.FilledQuantity() != 2 || order.RemainingQuantity() != 8 {
		t.Fatalf("after undo: state=%s filled=%d remaining=%d, want PartiallyFilled/2/8",
			order.State(), order.FilledQuantity(), order.RemainingQuantity())
	}
	// 恢复后可继续成交
	if err := oms.Fill(id, 8); err != nil {
		t.Fatalf("fill after undo failed: %v", err)
	}
	if oms.Order(id).State() != StateFilled {
		t.Fatalf("state = %s, want Filled", oms.Order(id).State())
	}

	// 历史已空：重复 undo 显式报错（幂等防护）
	if err := oms.UndoLast(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("second undoLast err = %v, want ErrNothingToUndo", err)
	}
}

func TestUndoModifyRestoresPreviousPriceAndQuantity(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Execute(NewModifyCommand(id, 101, 8)); err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	if err := oms.Execute(NewModifyCommand(id, 102, 9)); err != nil {
		t.Fatalf("second modify failed: %v", err)
	}
	if oms.Order(id).Price() != 102 || oms.Order(id).Quantity() != 9 {
		t.Fatalf("after modify: price=%d qty=%d, want 102/9",
			oms.Order(id).Price(), oms.Order(id).Quantity())
	}

	if err := oms.UndoLast(); err != nil {
		t.Fatalf("undoLast failed: %v", err)
	}
	order := oms.Order(id)
	if order.Price() != 101 || order.Quantity() != 8 || order.State() != StateSubmitted {
		t.Fatalf("after undo: price=%d qty=%d state=%s, want 101/8/Submitted",
			order.Price(), order.Quantity(), order.State())
	}
	if oms.HistoryLen() != 0 {
		t.Fatalf("history len = %d, want 0 after timeline reset", oms.HistoryLen())
	}
	if err := oms.UndoLast(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("second undoLast err = %v, want ErrNothingToUndo", err)
	}
}

func TestModifyQuantityBelowFilledRejected(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Fill(id, 5); err != nil {
		t.Fatalf("fill failed: %v", err)
	}

	// 改单数量必须大于已成交量，否则剩余量为负
	err := failureOf(t, func() error { return oms.Execute(NewModifyCommand(id, 100, 4)) })
	if err.Reason != ReasonModifyBelowFilled || err.NewQty != 4 || err.Filled != 5 {
		t.Fatalf("modify below filled err = %+v, want new 4 <= filled 5", err)
	}
	err = failureOf(t, func() error { return oms.Execute(NewModifyCommand(id, 100, 5)) })
	if err.Reason != ReasonModifyBelowFilled {
		t.Fatalf("modify equal filled err = %+v, want ModifyBelowFilled", err)
	}

	if err := oms.Execute(NewModifyCommand(id, 102, 6)); err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	if oms.Order(id).Price() != 102 || oms.Order(id).RemainingQuantity() != 1 { // 6 - 5
		t.Fatalf("after modify: price=%d remaining=%d, want 102/1",
			oms.Order(id).Price(), oms.Order(id).RemainingQuantity())
	}
}

func TestAuditTrailRecordsExecuteUndoAndFailures(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)     // seq=1
	if err := oms.Submit(id); err != nil { // seq=2
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Fill(id, 10); err != nil { // seq=3
		t.Fatalf("fill failed: %v", err)
	}
	_ = failureOf(t, func() error { return oms.Execute(NewCancelCommand(id)) }) // seq=4 失败也留痕
	id2 := oms.Create(SideSell, 101, 5)                                         // seq=5
	if err := oms.Submit(id2); err != nil {                                     // seq=6
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Execute(NewCancelCommand(id2)); err != nil { // seq=7
		t.Fatalf("cancel failed: %v", err)
	}
	if err := oms.UndoLast(); err != nil { // seq=8
		t.Fatalf("undoLast failed: %v", err)
	}

	log := oms.AuditLog()
	if len(log) != 8 {
		t.Fatalf("audit log len = %d, want 8", len(log))
	}
	wantContains := []struct {
		idx int
		sub string
	}{
		{0, "seq=1 CREATE order=1"},
		{3, "seq=4 EXEC CANCEL order=1 -> FAILED"},
		{3, "cannot cancel in state Filled"},
		{6, "seq=7 EXEC CANCEL order=2 -> OK"},
		{7, "seq=8 UNDO CANCEL order=2 -> OK"},
	}
	for _, w := range wantContains {
		if !strings.Contains(log[w.idx], w.sub) {
			t.Fatalf("log[%d] = %q, want contains %q", w.idx, log[w.idx], w.sub)
		}
	}
	// 回放到 seq=8 时订单 2 应为 Submitted（撤单被补偿）
	if oms.Order(id2).State() != StateSubmitted {
		t.Fatalf("state = %s, want Submitted", oms.Order(id2).State())
	}
}

func TestMementoSnapshotAndRestoreForRecovery(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if err := oms.Fill(id, 3); err != nil {
		t.Fatalf("fill failed: %v", err)
	}

	// 故障恢复场景：拍下当前快照后系统继续推进，再整单恢复到快照时刻
	snapshot := oms.Order(id).Snapshot()
	if !strings.Contains(snapshot.Summary(), "state=PartiallyFilled") ||
		!strings.Contains(snapshot.Summary(), "filled=3") {
		t.Fatalf("summary = %q, want state=PartiallyFilled and filled=3", snapshot.Summary())
	}

	if err := oms.Fill(id, 4); err != nil {
		t.Fatalf("fill failed: %v", err)
	}
	if err := oms.Execute(NewModifyCommand(id, 105, 12)); err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	if oms.Order(id).RemainingQuantity() != 5 { // 12 - 7
		t.Fatalf("remaining = %d, want 5", oms.Order(id).RemainingQuantity())
	}

	if err := oms.Restore(id, snapshot); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	order := oms.Order(id)
	if order.State() != StatePartiallyFilled || order.FilledQuantity() != 3 ||
		order.Price() != 100 || order.Quantity() != 10 {
		t.Fatalf("after restore: state=%s filled=%d price=%d qty=%d, want PartiallyFilled/3/100/10",
			order.State(), order.FilledQuantity(), order.Price(), order.Quantity())
	}
	if order.Version() != 6 {
		t.Fatalf("version after restore = %d, want 6", order.Version())
	}
	if oms.HistoryLen() != 0 {
		t.Fatalf("history len after restore = %d, want 0", oms.HistoryLen())
	}
	last := oms.AuditLog()[len(oms.AuditLog())-1]
	if !strings.Contains(last, "RESTORE order=1") {
		t.Fatalf("last log = %q, want contains RESTORE order=1", last)
	}
}

func TestRejectAfterPartialFillIsRejected(t *testing.T) {
	order := NewManagedOrder(1, SideBuy, 100, 10)
	if err := order.Submit(); err != nil {
		t.Fatal(err)
	}
	if err := order.Fill(2); err != nil {
		t.Fatal(err)
	}
	err := failureOf(t, order.Reject)
	if err.Reason != ReasonIllegalTransition || err.Action != "reject" ||
		err.From != StatePartiallyFilled || order.State() != StatePartiallyFilled {
		t.Fatalf("reject partial err=%+v state=%s", err, order.State())
	}
}

func TestUndoRejectsVersionConflictWithoutLosingFill(t *testing.T) {
	oms := NewOms()
	id := oms.Create(SideBuy, 100, 10)
	if err := oms.Submit(id); err != nil {
		t.Fatal(err)
	}
	if err := oms.Execute(NewModifyCommand(id, 101, 12)); err != nil {
		t.Fatal(err)
	}
	if err := oms.Fill(id, 3); err != nil {
		t.Fatal(err)
	}
	err := failureOf(t, oms.UndoLast)
	if err.Reason != ReasonUndoConflict || err.ExpectedVersion != 3 || err.ActualVersion != 4 {
		t.Fatalf("undo conflict err=%+v", err)
	}
	order := oms.Order(id)
	if order.Price() != 101 || order.Quantity() != 12 || order.FilledQuantity() != 3 ||
		order.State() != StatePartiallyFilled || oms.HistoryLen() != 1 {
		t.Fatalf("after failed undo: price=%d qty=%d filled=%d state=%s history=%d",
			order.Price(), order.Quantity(), order.FilledQuantity(), order.State(), oms.HistoryLen())
	}
}
